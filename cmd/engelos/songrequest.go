package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

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
