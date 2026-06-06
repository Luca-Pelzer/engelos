// Package youtube implements the YouTube Live Chat platform adapter for
// engelOS.
//
// The adapter speaks the YouTube Live Streaming REST API directly (net/http +
// encoding/json against https://www.googleapis.com/youtube/v3) rather than the
// google.golang.org/api SDK, which would pull a large grpc/protobuf dependency
// tree for the handful of endpoints this adapter needs. Chat is received by
// polling liveChatMessages.list and respecting the server-provided polling
// interval; messages, deletions and bans/timeouts are issued via the matching
// REST endpoints.
//
// This adapter is quota-bounded and opt-in. The liveChatMessages.list call
// costs 5 quota units and the default daily quota is only 10000 units, so
// continuous polling exhausts the quota within a few hours. The adapter
// therefore tracks consumed units against a configurable daily budget and
// stops polling gracefully (emitting a Disconnected event) before the budget
// is exceeded, resetting at the Pacific-time day boundary Google uses. The
// documented future upgrade for quota efficiency is the streaming
// liveChatMessages endpoint (gRPC), which is not implemented here.
//
// The adapter is safe for concurrent use: all internal state mutation is
// guarded by a mutex and the poll loop runs in its own goroutine that never
// blocks on a slow events consumer.
package youtube
