// Package mock provides an in-memory implementation of [adapters.Platform]
// for use in tests.
//
// The mock is fully driveable from test code: tests inject [adapters.Event]
// values via [Mock.EmitEvent], invoke the bot under test, and then assert
// against the slice returned by [Mock.Actions] to verify what the bot tried
// to do on the platform.
//
// The mock is safe for concurrent use: EmitEvent, Do, Actions and the
// channel returned by Events may all be accessed from different goroutines
// simultaneously.
package mock
