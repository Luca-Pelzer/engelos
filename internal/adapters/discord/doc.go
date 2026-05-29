// Package discord implements [adapters.Platform] on top of the Discord
// Gateway/REST APIs via github.com/bwmarrin/discordgo.
//
// The adapter is constructed with [New] and a [Config]. After [Adapter.Connect]
// it streams normalized [adapters.Event] values on the channel returned by
// [Adapter.Events] and accepts [adapters.Action] requests through
// [Adapter.Do]. The implementation is safe for concurrent use.
//
// # Token handling
//
// Discord (unlike Twitch IRC) has no anonymous mode, so [Adapter.Connect]
// fails fast with a clear error when [Config.Token] is empty. The token may
// be provided with or without the conventional "Bot " prefix; the adapter
// adds the prefix if missing.
//
// # Channels and guilds
//
// Action routing requires a guild id. The adapter learns the channel→guild
// mapping passively, from message events delivered after [Adapter.Connect].
// Until a message arrives in a given channel, ban/timeout actions targeted
// at that channel will fail with a descriptive error.
//
// # Event buffering
//
// The events channel is buffered to 256. If the consumer falls behind, new
// events are dropped (and a warning is logged) rather than blocking the
// Discord gateway goroutine.
package discord
