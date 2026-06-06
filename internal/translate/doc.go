// Package translate provides the per-(tenant, channel) configuration store for
// the chat-translation feature: whether translation is enabled, the target
// language, how the translation is posted back, and a minimum word-count gate.
//
// It is deliberately small and persistence-only; it stores and validates
// configuration and nothing more. HTTP clients, language detection, and
// orchestration live elsewhere so this store stays a focused sibling of the
// songrequests and featureflags stores.
package translate
