package commands

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// defaultSongRequestUserCooldown throttles per-viewer !sr spam. Song requests
// hit an external music API, so a slightly longer per-user cooldown than the
// admin default keeps a single chatter from flooding the queue.
const defaultSongRequestUserCooldown = 5 * time.Second

// SongOutcome is a sentinel enum the song-request commands react to without
// importing the provider/HTTP packages, mirroring [LoyaltyError] and
// [PredictionOutcome]. main wraps a music provider and maps its results onto
// these so internal/commands stays decoupled from Spotify/YouTube SDKs.
type SongOutcome int

const (
	// SongOK signals a successful operation.
	SongOK SongOutcome = iota
	// SongUnavailable means song requests are off for this channel or the
	// provider/back-end call failed.
	SongUnavailable
	// SongNotFound means no track matched the viewer's query.
	SongNotFound
	// SongInvalid means the request was empty or malformed.
	SongInvalid
	// SongTooLong means the matched track exceeds the channel's max-duration.
	SongTooLong
	// SongNothingPlaying means a now-playing/skip request found nothing active.
	SongNothingPlaying
)

// SongTrack is the read view a reply renders after a successful request.
type SongTrack struct {
	Title  string
	Artist string
}

// SongRequester is the narrow surface the song-request commands need. main
// wires a per-channel music provider (e.g. Spotify) onto it. The interface
// lives HERE so internal/commands never imports the provider/HTTP packages,
// mirroring [LoyaltyProvider] and [PredictionController].
//
// channel is passed through from msg.Channel; the provider resolves it to its
// own per-channel config (token, playlist, max-duration).
type SongRequester interface {
	// Request enqueues the track best matching query (a search string or a
	// track URL/URI) and returns the queued track on success.
	Request(ctx context.Context, channel, query string) (SongTrack, SongOutcome)
	// NowPlaying returns the track currently playing.
	NowPlaying(ctx context.Context, channel string) (SongTrack, SongOutcome)
	// Skip advances to the next track (mods only at the command layer).
	Skip(ctx context.Context, channel string) SongOutcome
}

// NewSongRequestCommand returns "!sr" (alias "songrequest"). Open to everyone.
//
// Usage: "!sr <song name or Spotify link>". Enqueues the best match on the
// channel's configured provider. A nil requester yields "song requests are
// unavailable".
func NewSongRequestCommand(req SongRequester) Command {
	return Command{
		Name:         "sr",
		Aliases:      []string{"songrequest"},
		Help:         "Request a song: !sr <song name or link>.",
		MinRole:      RoleEveryone,
		UserCooldown: defaultSongRequestUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if req == nil {
				return Reply{Text: fmt.Sprintf("%ssong requests are unavailable", mentionPrefix(msg))}
			}
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return Reply{Text: fmt.Sprintf("%susage: !sr <song name or link>", mentionPrefix(msg))}
			}
			track, status := req.Request(ctx, msg.Channel, query)
			switch status {
			case SongOK:
				return Reply{Text: fmt.Sprintf("%squeued: %s", mentionPrefix(msg), formatTrack(track))}
			case SongNotFound:
				return Reply{Text: fmt.Sprintf("%scouldn't find that song", mentionPrefix(msg))}
			case SongTooLong:
				return Reply{Text: fmt.Sprintf("%sthat track is too long for requests", mentionPrefix(msg))}
			case SongInvalid:
				return Reply{Text: fmt.Sprintf("%susage: !sr <song name or link>", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%scouldn't queue that right now", mentionPrefix(msg))}
			}
		},
	}
}

// NewNowPlayingCommand returns "!song" (alias "nowplaying"). Open to everyone.
// Reports the currently playing track. A nil requester yields "song requests
// are unavailable".
func NewNowPlayingCommand(req SongRequester) Command {
	return Command{
		Name:         "song",
		Aliases:      []string{"nowplaying"},
		Help:         "Show the currently playing song.",
		MinRole:      RoleEveryone,
		UserCooldown: defaultSongRequestUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if req == nil {
				return Reply{Text: fmt.Sprintf("%ssong requests are unavailable", mentionPrefix(msg))}
			}
			track, status := req.NowPlaying(ctx, msg.Channel)
			switch status {
			case SongOK:
				return Reply{Text: fmt.Sprintf("%snow playing: %s", mentionPrefix(msg), formatTrack(track))}
			case SongNothingPlaying:
				return Reply{Text: fmt.Sprintf("%snothing is playing right now", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%scouldn't read what's playing right now", mentionPrefix(msg))}
			}
		},
	}
}

// NewSkipSongCommand returns "!skipsong". Mods-only. Advances to the next
// track. A nil requester yields "song requests are unavailable".
func NewSkipSongCommand(req SongRequester) Command {
	return Command{
		Name:         "skipsong",
		Help:         "Skip the current song (mods).",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if req == nil {
				return Reply{Text: fmt.Sprintf("%ssong requests are unavailable", mentionPrefix(msg))}
			}
			switch req.Skip(ctx, msg.Channel) {
			case SongOK:
				return Reply{Text: fmt.Sprintf("%sskipped the current song", mentionPrefix(msg))}
			case SongNothingPlaying:
				return Reply{Text: fmt.Sprintf("%snothing is playing right now", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%scouldn't skip right now", mentionPrefix(msg))}
			}
		},
	}
}

// formatTrack renders a track as "Title by Artist", or just the title when no
// artist is known.
func formatTrack(t SongTrack) string {
	title := strings.TrimSpace(t.Title)
	artist := strings.TrimSpace(t.Artist)
	if artist == "" {
		return title
	}
	return title + " by " + artist
}
