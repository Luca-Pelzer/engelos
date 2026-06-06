package tts

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Luca-Pelzer/engelos/internal/tts/elevenlabs"
)

// jobBuffer bounds the per-service synthesis queue. A streamer never queues
// hundreds of alerts at once; a small buffer absorbs short bursts and drops the
// overflow rather than stalling the dispatcher.
const jobBuffer = 32

// playEventType is the WebSocket envelope type the overlay listens for.
const playEventType = "tts.play"

// SynthClient is the synthesis dependency. *elevenlabs.Client satisfies it.
type SynthClient interface {
	Synthesize(ctx context.Context, voiceID, text string) ([]byte, error)
}

// Secrets decrypts the at-rest API key. *secrets.Box satisfies it.
type Secrets interface {
	DecryptString(blob []byte) (string, error)
}

// Broadcaster pushes a typed envelope to all overlay clients.
// *runtime.WSBroadcaster satisfies it.
type Broadcaster interface {
	Broadcast(eventType string, payload any)
}

// playPayload is the data half of the tts.play envelope. Audio is base64 MP3.
type playPayload struct {
	Audio  string `json:"audio"`
	Format string `json:"format"`
}

// job is a single queued synthesis request.
type job struct {
	channel string
	text    string
}

// Service synthesizes speech for stream events and broadcasts it to overlays.
// Construct it with [NewService] and run [Service.Start]; stop it with
// [Service.Stop].
type Service struct {
	store       Store
	secrets     Secrets
	broadcaster Broadcaster
	tenantID    string
	logger      *slog.Logger

	// newClient builds a synth client bound to a channel's decrypted key. It
	// is a field so tests can inject a fake; the default builds a real
	// elevenlabs client.
	newClient func(apiKey, model string) SynthClient

	jobs   chan job
	wg     sync.WaitGroup
	stop   chan struct{}
	stopMu sync.Mutex
	closed bool
}

// NewService constructs a TTS service. store, secrets and broadcaster are
// required; a nil logger falls back to [slog.Default]. The worker does not run
// until [Service.Start] is called.
func NewService(store Store, secrets Secrets, broadcaster Broadcaster, tenantID string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		store:       store,
		secrets:     secrets,
		broadcaster: broadcaster,
		tenantID:    strings.TrimSpace(tenantID),
		logger:      logger.With("component", "tts.service"),
		jobs:        make(chan job, jobBuffer),
		stop:        make(chan struct{}),
	}
	s.newClient = func(apiKey, model string) SynthClient {
		return elevenlabs.New(
			elevenlabs.WithAPIKey(apiKey),
			elevenlabs.WithModel(model),
			elevenlabs.WithLogger(s.logger),
		)
	}
	return s
}

// Start launches the single worker goroutine. It returns immediately. The
// worker stops when ctx is cancelled or [Service.Stop] is called.
func (s *Service) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop signals the worker to exit and waits for it. It is idempotent.
func (s *Service) Stop() {
	s.stopMu.Lock()
	if !s.closed {
		s.closed = true
		close(s.stop)
	}
	s.stopMu.Unlock()
	s.wg.Wait()
}

func (s *Service) run(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case j := <-s.jobs:
			s.process(ctx, j)
		}
	}
}

// OnSubscribe enqueues a spoken sub alert for channel. It is nil-safe and
// non-blocking: a nil service or a full queue is a no-op (the latter logs).
// This is the seam the dispatcher calls on EventUserSubscribed.
func (s *Service) OnSubscribe(channel, username string) {
	if s == nil {
		return
	}
	s.enqueue(channel, formatSubText(username))
}

// OnResubscribe enqueues a spoken resub alert carrying the cumulative month
// count. It is nil-safe and non-blocking.
func (s *Service) OnResubscribe(channel, username string, months int) {
	if s == nil {
		return
	}
	s.enqueue(channel, formatResubText(username, months))
}

// OnGiftSubscribe enqueues a spoken gift-sub alert. It is nil-safe and
// non-blocking.
func (s *Service) OnGiftSubscribe(channel, gifter string) {
	if s == nil {
		return
	}
	s.enqueue(channel, formatGiftSubText(gifter))
}

// OnRaid enqueues a spoken raid alert carrying the incoming viewer count. It is
// nil-safe and non-blocking.
func (s *Service) OnRaid(channel, fromUsername string, viewers int) {
	if s == nil {
		return
	}
	s.enqueue(channel, formatRaidText(fromUsername, viewers))
}

// Speak enqueues arbitrary text to be spoken on channel, used by triggers that
// already carry the exact line (for example a channel-point reward). It is
// nil-safe and non-blocking.
func (s *Service) Speak(channel, text string) {
	if s == nil {
		return
	}
	s.enqueue(channel, text)
}

func (s *Service) enqueue(channel, text string) {
	channel = strings.TrimSpace(channel)
	text = strings.TrimSpace(text)
	if channel == "" || text == "" {
		return
	}
	select {
	case s.jobs <- job{channel: channel, text: text}:
	default:
		s.logger.Warn("tts job queue full; dropping", "channel", channel)
	}
}

// process runs one synthesis: load config, gate on enabled+voice, decrypt the
// key, synthesize, and broadcast. Every failure is logged and swallowed so a
// bad key or API hiccup never affects the rest of the bot.
func (s *Service) process(ctx context.Context, j job) {
	cfg, err := s.store.GetOrDefault(ctx, s.tenantID, j.channel)
	if err != nil {
		s.logger.Warn("tts config load failed", "channel", j.channel, "err", err)
		return
	}
	if !cfg.Enabled || cfg.VoiceID == "" {
		return
	}
	if len(cfg.APIKeyCiphertext) == 0 {
		s.logger.Warn("tts enabled but no api key set", "channel", j.channel)
		return
	}

	apiKey, err := s.secrets.DecryptString(cfg.APIKeyCiphertext)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		s.logger.Warn("tts api key decrypt failed", "channel", j.channel, "err", err)
		return
	}

	client := s.newClient(apiKey, cfg.Model)
	audio, err := client.Synthesize(ctx, cfg.VoiceID, sanitizeForSpeech(j.text))
	if err != nil {
		s.logger.Warn("tts synthesize failed", "channel", j.channel, "err", err)
		return
	}
	if len(audio) == 0 {
		return
	}

	if s.broadcaster == nil {
		return
	}
	s.broadcaster.Broadcast(playEventType, playPayload{
		Audio:  base64.StdEncoding.EncodeToString(audio),
		Format: "mp3",
	})
}

// formatSubText renders the spoken line for a subscription alert.
func formatSubText(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return fmt.Sprintf("%s just subscribed!", username)
}

// formatResubText renders the spoken line for a resubscription, mentioning the
// month count when it is known.
func formatResubText(username string, months int) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	if months > 1 {
		return fmt.Sprintf("%s just resubscribed for %d months!", username, months)
	}
	return fmt.Sprintf("%s just resubscribed!", username)
}

// formatGiftSubText renders the spoken line for a gift subscription.
func formatGiftSubText(gifter string) string {
	gifter = strings.TrimSpace(gifter)
	if gifter == "" {
		return "Someone just gifted a subscription!"
	}
	return fmt.Sprintf("%s just gifted a subscription!", gifter)
}

// formatRaidText renders the spoken line for an incoming raid, mentioning the
// viewer count when it is known.
func formatRaidText(fromUsername string, viewers int) string {
	fromUsername = strings.TrimSpace(fromUsername)
	if fromUsername == "" {
		return ""
	}
	if viewers > 0 {
		return fmt.Sprintf("%s is raiding with %d viewers!", fromUsername, viewers)
	}
	return fmt.Sprintf("%s is raiding!", fromUsername)
}

// sanitizeForSpeech is the normalization seam. For the MVP it passes text
// through unchanged; it exists so future number/emote normalization has a
// single home without touching callers.
func sanitizeForSpeech(text string) string {
	return text
}
