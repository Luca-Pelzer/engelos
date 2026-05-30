// Package youtube is a thin, pure-HTTP client for the parts of the YouTube
// Data API v3 that engelOS song-requests needs: resolving a viewer's request
// into a concrete video and reading that video's metadata.
//
// # API-key authentication
//
// Unlike the OAuth-based write endpoints, the read endpoints this package uses
// (search.list and videos.list) authenticate with a single Google API key.
// The key is supplied once to [New] and added as the "key" query parameter on
// every request; this package performs NO OAuth, token refresh or token
// storage. An invalid key surfaces as [ErrUnauthorized] and a spent daily
// quota as [ErrQuotaExceeded].
//
// # Song-request flow
//
// [Client.Search] turns a free-text request into a slice of [Video] candidates.
// Because the search snippet carries no duration, the chosen candidate is
// resolved with [Client.GetVideo], which fetches the contentDetails and parses
// the ISO 8601 duration into [Video.DurationMS]. [ParseVideoID] lets the bot
// accept a pasted link (watch URL, youtu.be short URL or /shorts/ URL) or a
// bare 11-character ID and feed the extracted ID straight into [Client.GetVideo]
// for the bot-managed song-request queue.
//
// # Neutral types
//
// Responses are decoded into the package's own [Video] view rather than leaking
// the API's wire shapes. The client imports nothing under engelos/internal and
// depends only on the Go standard library, so it can evolve independently of
// the rest of the bot.
//
// # Errors
//
// Non-2xx responses map onto sentinel errors comparable with [errors.Is]:
// [ErrUnauthorized] (401, or a non-quota 403 such as keyInvalid),
// [ErrQuotaExceeded] (403 with reason "quotaExceeded"), [ErrNotFound]
// ([Client.GetVideo] on an empty item list) and [ErrAPI] for any other non-2xx.
package youtube
