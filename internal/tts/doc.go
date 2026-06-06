// Package tts turns stream events into spoken audio. A per-channel [Store]
// holds the opt-in switch, the ElevenLabs voice and the encrypted
// bring-your-own-key; a [Service] subscribes to stream events (subscriptions
// for the MVP), synthesizes speech server-side via an ElevenLabs client, and
// broadcasts the resulting MP3 to the OBS overlay over the existing WebSocket.
//
// # Why server-side synthesis
//
// The ElevenLabs key is a per-channel secret. It is stored encrypted (the
// service owns a [Secrets] box) and only ever used inside the service. The
// overlay receives base64 audio, never the key.
//
// # Serialized worker
//
// Events can arrive in bursts (a sub and a donation within a second). Each
// channel synthesizes through a single worker goroutine so outbound ElevenLabs
// calls never overlap, which both keeps audio sequential and avoids tripping
// the key's concurrency limit. Enqueue is non-blocking: when the buffer is
// full the job is dropped with a warning, so event handling never stalls.
package tts
