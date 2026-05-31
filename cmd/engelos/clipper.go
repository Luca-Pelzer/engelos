package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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

// autoClipper adapts the clipper.Detector to runtime.ClipDetector. It feeds
// chat/sub/raid signals to the detector and, on a fire, asynchronously captures
// a Twitch clip, generates a Claude title, and announces the link. The async
// capture keeps the dispatcher hot path unblocked; clip creation polling can
// take up to 15 seconds.
type autoClipper struct {
	det      *clipper.Detector
	creator  clipCreator
	titler   clipTitler
	sender   clipAnnouncer
	logger   *slog.Logger
	now      func() time.Time
	channels map[string]bool // logins allowed to auto-clip; empty means all
}

func newAutoClipper(creator clipCreator, titler clipTitler, sender clipAnnouncer, channels []string, logger *slog.Logger) *autoClipper {
	allow := make(map[string]bool, len(channels))
	for _, c := range channels {
		allow[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c), "#"))] = true
	}
	return &autoClipper{
		det:      clipper.New(clipper.DefaultOptions()),
		creator:  creator,
		titler:   titler,
		sender:   sender,
		logger:   logger,
		now:      time.Now,
		channels: allow,
	}
}

func (a *autoClipper) enabled(channel string) bool {
	if len(a.channels) == 0 {
		return true
	}
	return a.channels[strings.ToLower(strings.TrimPrefix(channel, "#"))]
}

func (a *autoClipper) Message(_ context.Context, channel, userID, _, text string) {
	if !a.enabled(channel) {
		return
	}
	if fired, reason := a.det.Message(channel, userID, text, a.now()); fired {
		a.capture(channel, reason)
	}
}

func (a *autoClipper) Sub(_ context.Context, channel string) {
	if !a.enabled(channel) {
		return
	}
	if fired, reason := a.det.Sub(channel, a.now()); fired {
		a.capture(channel, reason)
	}
}

func (a *autoClipper) Raid(_ context.Context, channel string, viewers int) {
	if !a.enabled(channel) {
		return
	}
	if fired, reason := a.det.Raid(channel, viewers, a.now()); fired {
		a.capture(channel, reason)
	}
}

// capture runs the clip creation, titling and announcement in its own
// goroutine with an independent timeout so it never blocks the dispatcher.
func (a *autoClipper) capture(channel string, reason clipper.Reason) {
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
