// Package auth implements authentication, authorization, and credential
// management for engelOS. It is intentionally usable in both self-hosted
// (local) and cloud deployments - auth is mandatory even for self-hosters
// because a single bot instance is typically shared with moderators.
//
// The package provides:
//
//   - A User model with multi-tenant isolation, Argon2id password hashing
//     and optional TOTP-based 2FA.
//   - A four-level RBAC model (Owner, Admin, Mod, Viewer) implemented as
//     coarse Role values that expand into fine-grained Permission strings.
//   - Opaque, hashed session tokens (32-byte random, SHA-256 at rest) with
//     audit metadata (UserAgent, RemoteIP, LastUsedAt).
//   - Scoped, revocable API keys (prefix "eos_"), shown only once on
//     creation, hashed in storage, with optional ExpiresAt, IPWhitelist
//     and per-key RateLimit.
//   - A storage abstraction (Store) and a pure-Go SQLite implementation
//     based on modernc.org/sqlite (no CGO).
//
// All credentials are stored only as cryptographic hashes; plaintext
// secrets never leave this package's API surface (they are returned to the
// caller exactly once at creation time and are not persisted). All hash
// comparisons use crypto/subtle to avoid timing oracles.
//
// This package deliberately does not implement HTTP middleware, cookie
// handling, OAuth flows, or any transport-level concerns. Those belong
// to the api package which builds on top of this one.
package auth
