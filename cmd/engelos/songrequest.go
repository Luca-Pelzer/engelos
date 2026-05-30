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
	"github.com/Luca-Pelzer/engelos/internal/songrequests/spotify"
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
	req      spotifyRequester
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
	payload := nowPlayingPayload{Provider: "spotify"}
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
