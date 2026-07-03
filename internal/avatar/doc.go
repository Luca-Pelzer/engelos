// Package avatar is the stream AVATAR subsystem (Phase Z4/Z5): an in-process
// relay that streams TTS audio and avatar directives from the workflow engine
// to OBS browser-source overlay pages over WebSockets.
//
// The [Hub] holds a per-channel set of overlay subscribers (multiple overlays
// per channel are fine) and fans out three kinds of JSON message: a "speak"
// directive carrying synthesized audio, an "expression" directive selecting an
// avatar pose, and an "end" marker. Fan-out is non-blocking with a bounded
// per-subscriber send buffer; a slow or dead overlay is dropped rather than
// allowed to grow memory without bound.
//
// Overlay pages authenticate with a per-channel bearer token minted and stored
// by [TokenStore] and read by the owner via [TokenHandler]; the WebSocket
// endpoint served by [WSHandler] validates that token before upgrading. The
// token is the only secret on the wire — audio is embedded as base64 MP3 and no
// API key ever crosses the boundary.
package avatar
