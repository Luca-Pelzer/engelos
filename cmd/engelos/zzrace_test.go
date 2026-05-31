package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/clipper"
)

type fakeCreator struct{}

func (fakeCreator) CreateClip(context.Context, string, float64) (twitch.ClipView, error) {
	return twitch.ClipView{}, twitch.ErrHelixUnavailable
}

func (fakeCreator) GetClip(context.Context, string) (twitch.ClipView, error) {
	return twitch.ClipView{}, twitch.ErrHelixUnavailable
}

func TestAutoClipper_ConcurrentResolveAndMessage(t *testing.T) {
	store := &fakeClipStore{cfg: map[string]clipper.Config{
		"chan0": {TenantID: "t1", Channel: "chan0", Settings: clipper.Settings{Enabled: true, KeywordThreshold: 3}},
		"chan1": {TenantID: "t1", Channel: "chan1", Settings: clipper.Settings{Enabled: true}},
	}}
	a := newAutoClipper(store, clipper.DefaultOptions(), "t1", fakeCreator{}, nil, nil, nil, quietLogger())

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ch := fmt.Sprintf("chan%d", g%2)
			for i := 0; i < 200; i++ {
				a.Message(context.Background(), ch, fmt.Sprintf("u%d-%d", g, i), "", "clip it")
			}
		}(g)
	}
	wg.Wait()
}
