// Package migrate parses custom-command exports from Nightbot and
// StreamElements into engelOS's neutral [Command] and [Timer] records, so a
// streamer can switch bots without re-entering their commands.
//
// # Tolerant parsing
//
// Both tools export JSON, either as a top-level array or wrapped in an object.
// The parsers accept both shapes, fill sensible defaults for missing optional
// fields, and add a note to [Result.Skipped] for any entry that cannot be
// mapped (missing name/response, or a duplicate trigger) instead of failing the
// whole import. Invalid JSON or empty input is a hard error.
//
// # Access-level mapping
//
// Nightbot's userLevel (owner/moderator/subscriber/regular/everyone) and
// StreamElements' numeric accessLevel (1000/500/250/100) are mapped onto the
// engelOS roles broadcaster/moderator/subscriber/everyone. StreamElements'
// cooldown may be a number or a {user,global} object; the larger value is used.
//
// # Assumed export shapes
//
// Nightbot custom-command entries are assumed to carry "name", "message",
// "coolDown" and "userLevel". StreamElements entries are assumed to carry
// "command", "reply", "cooldown" (number or {user,global}) and "accessLevel".
// Unknown fields are ignored, and [Parse] with an empty source sniffs these
// distinctive field names to auto-detect the tool. The package imports nothing
// under engelos/internal and does no I/O.
package migrate
