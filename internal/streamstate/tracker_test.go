package streamstate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTracker_DefaultsOffline(t *testing.T) {
	tr := New()
	assert.False(t, tr.IsLive("chan"))
}

func TestTracker_SetAndGetNormalised(t *testing.T) {
	tr := New()
	tr.Set("Broadcaster", true)
	assert.True(t, tr.IsLive("broadcaster"))
	assert.True(t, tr.IsLive("#Broadcaster"))
	assert.True(t, tr.IsLive("  broadcaster  "))

	tr.Set("broadcaster", false)
	assert.False(t, tr.IsLive("broadcaster"))
}

func TestTracker_EmptyChannelIgnored(t *testing.T) {
	tr := New()
	tr.Set("", true)
	assert.False(t, tr.IsLive(""))
}
