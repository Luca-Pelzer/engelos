package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/clipper"
)

// clipCreator is the narrow Twitch surface the auto-clipper needs: capture a
// clip and resolve its public URL once processing finishes. The twitch.Adapter
// satisfies it.
type clipCreator interface {
	CreateClip(ctx context.Context, login string, durationSeconds float64) (twitch.ClipView, error)
	GetClip(ctx context.Context, clipID string) (twitch.ClipView, error)
}

// clipTitler generates a short clip title from the trigger context. The Claude
// client satisfies it via Complete.
type clipTitler interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
}

// clipAnnouncer posts the finished clip link to chat. platformSender satisfies it.
type clipAnnouncer interface {
	Send(ctx context.Context, channel, message string) error
}

// clipSettingsStore is the narrow per-channel-config surface the auto-clipper
// reads. clipper.Store satisfies it. It is optional: a nil store keeps the
// detector on env/base options for every allowed channel (the prior behaviour).
type clipSettingsStore interface {
	Get(ctx context.Context, tenantID, channel string) (clipper.Config, error)
}

// channelDetector caches a per-channel detector together with the moment it was
// (re)built, so settings edits from the dashboard are picked up after a short
// TTL without restarting and without a store read on every chat message.
type channelDetector struct {
	det      *clipper.Detector
	enabled  bool
	loadedAt time.Time
}

// settingsTTL bounds how stale a cached per-channel detector may be before the
// next event reloads it from the store. Tuning is infrequent, so a coarse TTL
// keeps the chat hot path free of per-message database reads.
const settingsTTL = 30 * time.Second

// autoClipper adapts the clipper.Detector to runtime.ClipDetector. It feeds
// chat/sub/raid signals to a PER-CHANNEL detector and, on a fire, asynchronously
// captures a Twitch clip, generates a Claude title, and announces the link. The
// async capture keeps the dispatcher hot path unblocked; clip creation polling
// can take up to 15 seconds.
//
// Each channel gets its own clipper.Detector built from a base [clipper.Options]
// with that channel's stored [clipper.Settings] merged on top, so a small
// channel can lower the unique-user thresholds (for example to 3) while a busy
// channel keeps the higher production defaults. Detectors are cached and
// refreshed every settingsTTL so dashboard edits apply without a restart.
type autoClipper struct {
	store    clipSettingsStore // optional; nil means base options for all
	base     clipper.Options
	tenantID string
	creator  clipCreator
	titler   clipTitler
	sender   clipAnnouncer
	logger   *slog.Logger
	now      func() time.Time
	channels map[string]bool // logins allowed to auto-clip; empty means all

	mu   sync.Mutex
	byCh map[string]*channelDetector
}

func newAutoClipper(store clipSettingsStore, base clipper.Options, tenantID string, creator clipCreator, titler clipTitler, sender clipAnnouncer, channels []string, logger *slog.Logger) *autoClipper {
	allow := make(map[string]bool, len(channels))
	for _, c := range channels {
		allow[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c), "#"))] = true
	}
	return &autoClipper{
		store:    store,
		base:     base,
		tenantID: tenantID,
		creator:  creator,
		titler:   titler,
		sender:   sender,
		logger:   logger,
		now:      time.Now,
		channels: allow,
		byCh:     make(map[string]*channelDetector),
	}
}

// allowedByEnv reports whether the env allow-list permits this channel. An empty
// allow-list means every channel is permitted, matching the prior behaviour.
func (a *autoClipper) allowedByEnv(channel string) bool {
	if len(a.channels) == 0 {
		return true
	}
	return a.channels[strings.ToLower(strings.TrimPrefix(channel, "#"))]
}

// resolve returns the per-channel detector and whether auto-clipping is enabled
// for the channel, (re)building the detector from stored settings when the cache
// is cold or older than settingsTTL.
//
// Gating layers: the env allow-list is the master switch (an unlisted channel is
// always off). When a stored config row exists it additionally supplies the
// per-channel enable flag and threshold overrides; with no row (or no store) the
// channel runs on the base options and is enabled by the env allow-list alone.
func (a *autoClipper) resolve(ctx context.Context, channel string) (*clipper.Detector, bool) {
	if !a.allowedByEnv(channel) {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cd := a.byCh[channel]; cd != nil && a.now().Sub(cd.loadedAt) < settingsTTL {
		return cd.det, cd.enabled
	}

	opts := a.base
	enabled := true
	if a.store != nil {
		cfg, err := a.store.Get(ctx, a.tenantID, channel)
		switch {
		case err == nil:
			opts = cfg.Settings.ApplyTo(a.base)
			enabled = cfg.Settings.Enabled
		case errors.Is(err, clipper.ErrNotFound):
			// No per-channel row: keep base options, enabled by env allow-list.
		default:
			a.logger.WarnContext(ctx, "autoclip: settings load failed, using base options",
				"channel", channel, "err", err)
		}
	}

	cd := &channelDetector{det: clipper.New(opts), enabled: enabled, loadedAt: a.now()}
	a.byCh[channel] = cd
	return cd.det, cd.enabled
}

func (a *autoClipper) Message(ctx context.Context, channel, userID, _, text string) {
	det, enabled := a.resolve(ctx, channel)
	if !enabled {
		return
	}
	if fired, reason := det.Message(channel, userID, text, a.now()); fired {
		a.capture(channel, reason)
	}
}

func (a *autoClipper) Sub(ctx context.Context, channel string) {
	det, enabled := a.resolve(ctx, channel)
	if !enabled {
		return
	}
	if fired, reason := det.Sub(channel, a.now()); fired {
		a.capture(channel, reason)
	}
}

func (a *autoClipper) Raid(ctx context.Context, channel string, viewers int) {
	det, enabled := a.resolve(ctx, channel)
	if !enabled {
		return
	}
	if fired, reason := det.Raid(channel, viewers, a.now()); fired {
		a.capture(channel, reason)
	}
}

// capture runs the clip creation, titling and announcement in its own
// goroutine with an independent timeout so it never blocks the dispatcher.
func (a *autoClipper) capture(channel string, reason clipper.Reason) {
	if a.creator == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clip, err := a.creator.CreateClip(ctx, channel, 30)
		if err != nil {
			// Anonymous/unauthenticated Twitch cannot create clips; log that
			// quietly at debug so a non-affiliate or token-less deployment is
			// not spammed with warnings on every hype moment.
			if errors.Is(err, twitch.ErrHelixUnavailable) {
				a.logger.DebugContext(ctx, "autoclip: skipped, twitch unavailable",
					"channel", channel, "reason", string(reason))
				return
			}
			a.logger.WarnContext(ctx, "autoclip: create failed",
				"channel", channel, "reason", string(reason), "err", err)
			return
		}

		url := a.resolveURL(ctx, clip.ID)
		title := a.title(ctx, channel, reason)

		msg := "Clip! " + title
		if url != "" {
			msg += " " + url
		}
		if err := a.sender.Send(ctx, channel, msg); err != nil {
			a.logger.WarnContext(ctx, "autoclip: announce failed", "channel", channel, "err", err)
		}
		a.logger.InfoContext(ctx, "autoclip: created",
			"channel", channel, "reason", string(reason), "clip_id", clip.ID, "url", url)
	}()
}

// resolveURL polls GetClip until Twitch returns a public URL or the attempts
// run out (clips process asynchronously; Twitch suggests up to ~15s).
func (a *autoClipper) resolveURL(ctx context.Context, clipID string) string {
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(3 * time.Second):
		}
		clip, err := a.creator.GetClip(ctx, clipID)
		if err == nil && clip.URL != "" {
			return clip.URL
		}
	}
	return ""
}

// title asks Claude for a short, punchy clip title, falling back to a generic
// label when the titler is unavailable or errors.
func (a *autoClipper) title(ctx context.Context, channel string, reason clipper.Reason) string {
	fallback := defaultClipTitle(reason)
	if a.titler == nil {
		return fallback
	}
	sys := "You name Twitch stream clips. Reply with ONLY a punchy clip title of at most 60 characters, " +
		"no quotation marks and no preamble."
	user := "A clip-worthy moment just happened on the channel. The trigger was: " + string(reason) +
		". Suggest a short, exciting title."
	out, err := a.titler.Complete(ctx, sys, user)
	if err != nil {
		a.logger.WarnContext(ctx, "autoclip: title failed", "channel", channel, "err", err)
		return fallback
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fallback
	}
	if r := []rune(out); len(r) > 60 {
		out = strings.TrimRight(string(r[:60]), " ")
	}
	return out
}

// defaultClipTitle is the generic title used when AI titling is unavailable.
func defaultClipTitle(reason clipper.Reason) string {
	switch reason {
	case clipper.ReasonRaid:
		return "Raid hype!"
	case clipper.ReasonSubBurst:
		return "Sub hype!"
	default:
		return "Chat went wild!"
	}
}
