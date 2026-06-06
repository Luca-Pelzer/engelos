// Package elevenlabs is a thin bring-your-own-key client for the ElevenLabs
// text-to-speech API. It mirrors the shape of internal/translate/claude: a
// small net/http wrapper with functional options, sentinel errors, and no
// dependencies under engelos/internal, so it can be reused and tested in
// isolation.
//
// The API key is supplied per client via [WithAPIKey] and sent as the
// xi-api-key header. Unlike the Claude proxy client the key is mandatory:
// every account brings its own ElevenLabs key, so [Client.Synthesize] returns
// [ErrAPIKeyRequired] when none is set.
//
// Synthesize returns raw MP3 bytes. The caller (the tts service) base64-encodes
// them and ships them to the OBS overlay over the existing WebSocket; the key
// never leaves the server.
package elevenlabs
