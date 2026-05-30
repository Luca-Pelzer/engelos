// Package spotify is a thin, pure-HTTP client for the parts of the Spotify Web
// API that engelOS song-requests needs, built around the "playlist as queue"
// approach.
//
// # Playlist as queue
//
// Instead of driving Spotify's native playback queue (which is awkward to
// inspect and mutate via the Web API), the streamer plays from a dedicated
// song-request playlist. The bot treats that playlist as the request queue: it
// appends requested tracks with [Client.AddToPlaylist], inspects the pending
// queue with [Client.PlaylistTracks], and prunes already-played tracks with
// [Client.RemoveFromPlaylist]. [Client.Search] and [Client.GetTrack] resolve a
// user's free-text request or pasted link into a concrete [Track], and
// [Client.NowPlaying] / [Client.Skip] cover the live-playback controls.
//
// # Tokens are injected, never stored
//
// This package performs NO OAuth, token refresh, or token storage. Every method
// takes the access token as a plain string parameter and sets it as an
// "Authorization: Bearer <token>" header. The caller is responsible for
// obtaining a fresh, valid token (refreshing it elsewhere) and passing it in
// per call. A 401 surfaces as [ErrUnauthorized] so the caller knows to refresh.
//
// # Neutral types
//
// Responses are decoded into the package's own [Track] view rather than leaking
// Spotify's wire shapes. The client imports nothing under engelos/internal and
// depends only on the Go standard library, so it can evolve independently of
// the rest of the bot.
//
// # Errors
//
// Non-2xx responses map onto sentinel errors comparable with [errors.Is]:
// [ErrUnauthorized] (401), [ErrPremiumRequired] (403 on player endpoints),
// [ErrNoActiveDevice] (404 on player endpoints) and [ErrAPI] for any other
// non-2xx. [Client.NowPlaying] reports "nothing playing" (HTTP 204) by
// returning ok=false rather than an error.
package spotify
