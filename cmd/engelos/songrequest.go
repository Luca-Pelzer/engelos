package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/songrequests"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/spotify"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/youtube"
)

// spotifyRequester implements commands.SongRequester for the Spotify
// "playlist as queue" approach. It resolves per-channel config from the
// songrequests store, fetches the bot's decrypted Spotify token from the auth
// store on each call (the background refresher keeps it fresh), and drives the
// Spotify Web API client. It is decoupled: internal/commands sees only the
// SongRequester interface, never Spotify types.
//
// The token is read per-call rather than cached so a refresh by the background
// refresher takes effect immediately; an expired token surfaces as
// SongUnavailable (the next refresh cycle repairs it).
type spotifyRequester struct {
	cfg      songrequests.Store
	client   *spotify.Client
	auth     auth.Store
	tenantID string
	logger   *slog.Logger
}

// channelConfig loads the per-channel song-request config and reports whether
// Spotify is the enabled provider with a playlist configured.
func (r spotifyRequester) channelConfig(ctx context.Context, channel string) (songrequests.Config, bool) {
	c, err := r.cfg.GetOrDefault(ctx, r.tenantID, channel)
	if err != nil {
		r.logger.WarnContext(ctx, "songrequest: config lookup failed", "channel", channel, "err", err)
		return songrequests.Config{}, false
	}
	if !c.Enabled || c.Provider != "spotify" || strings.TrimSpace(c.SpotifyPlaylistID) == "" {
		return songrequests.Config{}, false
	}
	return c, true
}

// token fetches the bot's current decrypted Spotify access token.
func (r spotifyRequester) token(ctx context.Context) (string, bool) {
	id, err := r.auth.GetBotIdentity(ctx, r.tenantID, auth.ProviderSpotify)
	if err != nil || strings.TrimSpace(id.AccessToken) == "" {
		return "", false
	}
	return id.AccessToken, true
}

// Request resolves the query (URL/URI or search text) to a track, enforces the
// channel's max-duration, and appends it to the configured Spotify playlist.
func (r spotifyRequester) Request(ctx context.Context, channel, query string) (commands.SongTrack, commands.SongOutcome) {
	cfg, ok := r.channelConfig(ctx, channel)
	if !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	token, ok := r.token(ctx)
	if !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}

	var track spotify.Track
	if id, isURL := spotify.ParseTrackID(query); isURL {
		t, err := r.client.GetTrack(ctx, token, id)
		if err != nil {
			return commands.SongTrack{}, mapSpotifyErr(err)
		}
		track = t
	} else {
		results, err := r.client.Search(ctx, token, query, 1)
		if err != nil {
			return commands.SongTrack{}, mapSpotifyErr(err)
		}
		if len(results) == 0 {
			return commands.SongTrack{}, commands.SongNotFound
		}
		track = results[0]
	}

	if cfg.MaxDurationSec > 0 && track.DurationMS > cfg.MaxDurationSec*1000 {
		return commands.SongTrack{}, commands.SongTooLong
	}
	if err := r.client.AddToPlaylist(ctx, token, cfg.SpotifyPlaylistID, track.URI); err != nil {
		return commands.SongTrack{}, mapSpotifyErr(err)
	}
	return commands.SongTrack{Title: track.Name, Artist: track.Artist}, commands.SongOK
}

// NowPlaying reports the bot account's currently playing track.
func (r spotifyRequester) NowPlaying(ctx context.Context, channel string) (commands.SongTrack, commands.SongOutcome) {
	if _, ok := r.channelConfig(ctx, channel); !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	token, ok := r.token(ctx)
	if !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	track, playing, err := r.client.NowPlaying(ctx, token)
	if err != nil {
		return commands.SongTrack{}, mapSpotifyErr(err)
	}
	if !playing {
		return commands.SongTrack{}, commands.SongNothingPlaying
	}
	return commands.SongTrack{Title: track.Name, Artist: track.Artist}, commands.SongOK
}

// Skip advances the bot account's playback to the next track.
func (r spotifyRequester) Skip(ctx context.Context, channel string) commands.SongOutcome {
	if _, ok := r.channelConfig(ctx, channel); !ok {
		return commands.SongUnavailable
	}
	token, ok := r.token(ctx)
	if !ok {
		return commands.SongUnavailable
	}
	if err := r.client.Skip(ctx, token); err != nil {
		return mapSpotifyErr(err)
	}
	return commands.SongOK
}

// nowPlayingBroadcaster is the raw byte sink the poller publishes to; ws.Hub
// satisfies it. Declared here so the poller never imports internal/api/ws.
type nowPlayingBroadcaster interface {
	Broadcast(payload []byte)
}

// nowPlayingPoller periodically reads each channel's currently playing track
// and broadcasts a "song.now_playing" WebSocket envelope when it changes, so
// the /overlay/now-playing OBS overlay can render it live. Polling (rather
// than push) is necessary because Spotify has no now-playing webhook; the
// poller dedupes so an unchanged track is broadcast at most once.
type nowPlayingPoller struct {
	req      commands.SongRequester
	sink     nowPlayingBroadcaster
	channels []string
	interval time.Duration
	logger   *slog.Logger
	last     map[string]string
}

// nowPlayingEnvelope mirrors the {type,data} shape produced by
// runtime.WSBroadcaster so overlay assets handle every event uniformly.
type nowPlayingEnvelope struct {
	Type string            `json:"type"`
	Data nowPlayingPayload `json:"data"`
}

// nowPlayingPayload is the data the now-playing overlay renders.
type nowPlayingPayload struct {
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	IsPlaying bool   `json:"is_playing"`
	Provider  string `json:"provider"`
}

// run drives the poll loop until ctx is cancelled. It is started in a
// goroutine by main and returns on ctx.Done.
func (p *nowPlayingPoller) run(ctx context.Context) {
	if p.interval <= 0 {
		p.interval = 8 * time.Second
	}
	if p.last == nil {
		p.last = make(map[string]string)
	}
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, ch := range p.channels {
				p.poll(ctx, ch)
			}
		}
	}
}

// poll reads one channel's now-playing track and broadcasts it when the
// (title|artist|playing) tuple differs from the last broadcast for that
// channel. Errors degrade silently to a "stopped" state so a transient API
// blip clears the overlay rather than freezing a stale track.
func (p *nowPlayingPoller) poll(ctx context.Context, channel string) {
	track, outcome := p.req.NowPlaying(ctx, channel)
	payload := nowPlayingPayload{}
	if outcome == commands.SongOK {
		payload.Title = track.Title
		payload.Artist = track.Artist
		payload.IsPlaying = true
	}
	key := payload.Title + "\x00" + payload.Artist + "\x00" + boolKey(payload.IsPlaying)
	if p.last[channel] == key {
		return
	}
	p.last[channel] = key

	buf, err := json.Marshal(nowPlayingEnvelope{Type: "song.now_playing", Data: payload})
	if err != nil {
		p.logger.WarnContext(ctx, "now-playing: marshal failed", "channel", channel, "err", err)
		return
	}
	p.sink.Broadcast(buf)
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// mapSpotifyErr translates a Spotify client error into a chat-facing outcome.
// No active device / nothing playing both read as "nothing playing"; auth and
// premium failures degrade to "unavailable" (a refresh or Premium upgrade is
// the operator's fix, not something a viewer can act on).
func mapSpotifyErr(err error) commands.SongOutcome {
	switch {
	case errors.Is(err, spotify.ErrNoActiveDevice), errors.Is(err, spotify.ErrNotPlaying):
		return commands.SongNothingPlaying
	default:
		return commands.SongUnavailable
	}
}

// youtubeRequester implements commands.SongRequester for the YouTube
// "bot-managed queue" approach: requested videos are appended to a per-channel
// queue store, and the /overlay/song-player browser source promotes and plays
// them via the songqueue API. It resolves the search query (or a YouTube URL)
// through the YouTube Data API, enforces the channel's max-duration, then
// enqueues. Decoupled: internal/commands sees only the SongRequester interface.
type youtubeRequester struct {
	cfg      songrequests.Store
	queue    queue.Store
	client   *youtube.Client
	tenantID string
	logger   *slog.Logger
}

// channelEnabled reports whether YouTube is the enabled provider for channel.
func (r youtubeRequester) channelEnabled(ctx context.Context, channel string) (songrequests.Config, bool) {
	c, err := r.cfg.GetOrDefault(ctx, r.tenantID, channel)
	if err != nil {
		r.logger.WarnContext(ctx, "songrequest: config lookup failed", "channel", channel, "err", err)
		return songrequests.Config{}, false
	}
	if !c.Enabled || c.Provider != "youtube" {
		return songrequests.Config{}, false
	}
	return c, true
}

// Request resolves the query to a YouTube video (URL lookup or search),
// enforces max-duration, and enqueues it for the channel's player.
func (r youtubeRequester) Request(ctx context.Context, channel, query string) (commands.SongTrack, commands.SongOutcome) {
	cfg, ok := r.channelEnabled(ctx, channel)
	if !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}

	var vid youtube.Video
	if id, isURL := youtube.ParseVideoID(query); isURL {
		v, err := r.client.GetVideo(ctx, id)
		if err != nil {
			return commands.SongTrack{}, mapYouTubeErr(err)
		}
		vid = v
	} else {
		results, err := r.client.Search(ctx, query, 1)
		if err != nil {
			return commands.SongTrack{}, mapYouTubeErr(err)
		}
		if len(results) == 0 {
			return commands.SongTrack{}, commands.SongNotFound
		}
		// Search snippets carry no duration; fetch details so the
		// max-duration check and the player have a real length.
		v, err := r.client.GetVideo(ctx, results[0].ID)
		if err != nil {
			return commands.SongTrack{}, mapYouTubeErr(err)
		}
		vid = v
	}

	if cfg.MaxDurationSec > 0 && vid.DurationMS > cfg.MaxDurationSec*1000 {
		return commands.SongTrack{}, commands.SongTooLong
	}
	if _, err := r.queue.Enqueue(ctx, queue.Item{
		TenantID:   r.tenantID,
		Channel:    channel,
		VideoID:    vid.ID,
		Title:      vid.Title,
		Artist:     vid.Channel,
		DurationMS: vid.DurationMS,
	}); err != nil {
		r.logger.WarnContext(ctx, "songrequest: enqueue failed", "channel", channel, "err", err)
		return commands.SongTrack{}, commands.SongUnavailable
	}
	return commands.SongTrack{Title: vid.Title, Artist: vid.Channel}, commands.SongOK
}

// NowPlaying returns the queue's currently playing item.
func (r youtubeRequester) NowPlaying(ctx context.Context, channel string) (commands.SongTrack, commands.SongOutcome) {
	if _, ok := r.channelEnabled(ctx, channel); !ok {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	cur, err := r.queue.Current(ctx, r.tenantID, channel)
	if err != nil {
		if errors.Is(err, queue.ErrEmpty) {
			return commands.SongTrack{}, commands.SongNothingPlaying
		}
		return commands.SongTrack{}, commands.SongUnavailable
	}
	return commands.SongTrack{Title: cur.Title, Artist: cur.Artist}, commands.SongOK
}

// Skip marks the current item played; the player advances on its own poll, so
// the queue simply drops the current song.
func (r youtubeRequester) Skip(ctx context.Context, channel string) commands.SongOutcome {
	if _, ok := r.channelEnabled(ctx, channel); !ok {
		return commands.SongUnavailable
	}
	cur, err := r.queue.Current(ctx, r.tenantID, channel)
	if err != nil {
		if errors.Is(err, queue.ErrEmpty) {
			return commands.SongNothingPlaying
		}
		return commands.SongUnavailable
	}
	if err := r.queue.MarkPlayed(ctx, r.tenantID, channel, cur.ID); err != nil {
		return commands.SongUnavailable
	}
	return commands.SongOK
}

// mapYouTubeErr translates a YouTube client error into a chat-facing outcome.
func mapYouTubeErr(err error) commands.SongOutcome {
	switch {
	case errors.Is(err, youtube.ErrNotFound):
		return commands.SongNotFound
	default:
		return commands.SongUnavailable
	}
}

// multiProviderRequester routes each SongRequester call to the provider the
// channel has configured (spotify or youtube), so a single requester can serve
// channels that picked different backends. An unset/unknown provider yields
// SongUnavailable. It reads the per-channel config once per call; the wrapped
// providers re-check the config themselves, which is cheap and keeps each
// provider independently testable.
type multiProviderRequester struct {
	cfg      songrequests.Store
	spotify  commands.SongRequester
	youtube  commands.SongRequester
	tenantID string
	logger   *slog.Logger
}

// pick returns the provider for the channel, or nil when song requests are
// disabled or the provider is unknown/unconfigured.
func (m multiProviderRequester) pick(ctx context.Context, channel string) commands.SongRequester {
	c, err := m.cfg.GetOrDefault(ctx, m.tenantID, channel)
	if err != nil || !c.Enabled {
		return nil
	}
	switch c.Provider {
	case "spotify":
		return m.spotify
	case "youtube":
		return m.youtube
	default:
		return nil
	}
}

func (m multiProviderRequester) Request(ctx context.Context, channel, query string) (commands.SongTrack, commands.SongOutcome) {
	p := m.pick(ctx, channel)
	if p == nil {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	return p.Request(ctx, channel, query)
}

func (m multiProviderRequester) NowPlaying(ctx context.Context, channel string) (commands.SongTrack, commands.SongOutcome) {
	p := m.pick(ctx, channel)
	if p == nil {
		return commands.SongTrack{}, commands.SongUnavailable
	}
	return p.NowPlaying(ctx, channel)
}

func (m multiProviderRequester) Skip(ctx context.Context, channel string) commands.SongOutcome {
	p := m.pick(ctx, channel)
	if p == nil {
		return commands.SongUnavailable
	}
	return p.Skip(ctx, channel)
}
