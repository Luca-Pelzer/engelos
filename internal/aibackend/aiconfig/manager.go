package aiconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/aibackend"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/anthropic"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/openai"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

// Manager is a hot-swappable [aibackend.Backend]. It resolves the active
// backend from (in precedence order) the DB config store, the environment, then
// the provider defaults, and rebuilds the inner client on [Manager.Reload]
// without disturbing in-flight Complete/Translate calls. It is safe for
// concurrent use.
type Manager struct {
	// state holds the active backend and a masked view of the config that
	// built it. Reads (Complete/Translate) are lock-free via the atomic
	// pointer; Reload swaps the whole state in one Store.
	state atomic.Pointer[managerState]

	store    Store           // may be nil: env/default only, no persistence
	box      Crypto          // may be nil: no secrets key, stored keys unusable
	usage    *usage.Registry // may be nil: usage telemetry off
	tenantID string
	logger   *slog.Logger
}

// ManagerOption configures a [Manager] in [NewManager].
type ManagerOption func(*Manager)

// WithUsageRegistry records connection-test calls (labelled "test") into reg and
// exposes reg via [Manager.UsageSnapshot]. A nil reg is ignored.
func WithUsageRegistry(reg *usage.Registry) ManagerOption {
	return func(m *Manager) {
		if reg != nil {
			m.usage = reg
		}
	}
}

// managerState is an immutable snapshot of the active backend plus the resolved
// config that built it. A new value is created on every (re)load and published
// atomically, so readers never observe a torn config.
type managerState struct {
	backend aibackend.Backend
	// The fields below are the effective, display-ready config. apiKey is the
	// resolved plaintext, held in memory only to derive the masked hint and to
	// run the active connection test; it is never serialised.
	provider string
	baseURL  string
	model    string
	apiKey   string
	source   string
}

// Crypto is the minimal secrets surface the manager needs. *secrets.Box
// satisfies it. A nil Crypto means no secrets key is configured: stored keys
// cannot be decrypted and new keys cannot be persisted.
type Crypto interface {
	EncryptString(s string) ([]byte, error)
	DecryptString(blob []byte) (string, error)
}

// Config source labels reported by [Manager.Snapshot].
const (
	sourceDB      = "db"
	sourceEnv     = "env"
	sourceDefault = "default"
)

// ErrNoSecretsKey is returned by [Manager.UpdateConfig] when a caller tries to
// store an API key while no secrets key is configured (so it cannot be
// encrypted at rest).
var ErrNoSecretsKey = errors.New("aiconfig: no secrets key configured")

// ErrNoStore is returned by [Manager.UpdateConfig] when the manager has no
// persistence backend wired.
var ErrNoStore = errors.New("aiconfig: no config store configured")

// Compile-time proof the Manager is a drop-in backend for every AI consumer.
var _ aibackend.Backend = (*Manager)(nil)

// NewManager builds a Manager and resolves the initial backend. store and box
// may be nil (env/default-only, no persistence). A nil logger defaults to
// [slog.Default]. It returns an error only when a present store fails to read.
func NewManager(ctx context.Context, store Store, box Crypto, tenantID string, logger *slog.Logger, opts ...ManagerOption) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		store:    store,
		box:      box,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger.With("component", "aiconfig.manager"),
	}
	for _, opt := range opts {
		opt(m)
	}
	st, err := m.resolve(ctx)
	if err != nil {
		return nil, err
	}
	m.state.Store(st)
	return m, nil
}

// Complete implements [aibackend.Backend].
func (m *Manager) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	return m.state.Load().backend.Complete(ctx, systemPrompt, userText)
}

// Translate implements [aibackend.Backend].
func (m *Manager) Translate(ctx context.Context, text, targetLang string) (string, error) {
	return m.state.Load().backend.Translate(ctx, text, targetLang)
}

// UsageSnapshot returns the in-memory AI usage counters, or a zero snapshot when
// telemetry is not wired.
func (m *Manager) UsageSnapshot() usage.Snapshot {
	return m.usage.Snapshot()
}

// Reload re-resolves the active config (DB > env > default) and atomically
// swaps the inner backend. In-flight Complete/Translate calls keep using the
// backend they loaded; subsequent calls see the new one.
func (m *Manager) Reload(ctx context.Context) error {
	st, err := m.resolve(ctx)
	if err != nil {
		return err
	}
	m.state.Store(st)
	m.logger.InfoContext(ctx, "ai backend (re)loaded",
		"provider", st.provider, "source", st.source, "api_key_set", st.apiKey != "")
	return nil
}

// resolve computes the active state using DB > env > default precedence.
func (m *Manager) resolve(ctx context.Context) (*managerState, error) {
	if m.store != nil {
		stored, err := m.store.Get(ctx, m.tenantID)
		switch {
		case err == nil:
			return m.buildState(stored.Provider, stored.BaseURL, stored.Model,
				m.decryptKey(stored.APIKeyCiphertext), sourceDB), nil
		case errors.Is(err, ErrNotFound):
			// fall through to env
		default:
			return nil, fmt.Errorf("aiconfig: load config: %w", err)
		}
	}

	envCfg := aibackend.LoadConfig()
	if envCfg != (aibackend.Config{}) {
		return m.buildState(envCfg.Provider, envCfg.BaseURL, envCfg.Model, envCfg.APIKey, sourceEnv), nil
	}

	return m.buildState("", "", "", "", sourceDefault), nil
}

// buildState maps a resolved (provider, baseURL, model, plaintext key) tuple to
// a managerState: it rewrites catalog presets (groq/ollama) onto their wire and
// preset base, constructs the inner client, and records the effective,
// display-ready values.
func (m *Manager) buildState(provider, baseURL, model, apiKey, source string) *managerState {
	wire, presetBase := wireFor(provider)
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = presetBase
	}
	backendCfg := aibackend.Config{
		Provider: wire,
		BaseURL:  base,
		APIKey:   apiKey,
		Model:    strings.TrimSpace(model),
	}
	return &managerState{
		backend:  aibackend.New(backendCfg, m.logger),
		provider: displayProvider(provider),
		baseURL:  effectiveBase(wire, base),
		model:    effectiveModel(wire, model),
		apiKey:   apiKey,
		source:   source,
	}
}

// decryptKey best-effort recovers the plaintext stored key. A missing box or a
// decrypt failure logs and yields "" so the backend runs keyless rather than
// crashing.
func (m *Manager) decryptKey(cipher []byte) string {
	if len(cipher) == 0 {
		return ""
	}
	if m.box == nil {
		m.logger.Warn("ai config has a stored api key but no secrets key is configured; key unavailable")
		return ""
	}
	pt, err := m.box.DecryptString(cipher)
	if err != nil {
		m.logger.Warn("ai config stored api key decrypt failed", "err", err)
		return ""
	}
	return pt
}

// Snapshot is the masked, serialisable view of the active config for GET
// /ai/config. It never carries the plaintext or ciphertext key, only a short
// hint and a set flag.
type Snapshot struct {
	Provider   string `json:"provider"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	APIKeySet  bool   `json:"api_key_set"`
	APIKeyHint string `json:"api_key_hint,omitempty"`
	Source     string `json:"source"`
}

// Snapshot returns the masked active configuration.
func (m *Manager) Snapshot() Snapshot {
	st := m.state.Load()
	return Snapshot{
		Provider:   st.provider,
		BaseURL:    st.baseURL,
		Model:      st.model,
		APIKeySet:  st.apiKey != "",
		APIKeyHint: hintKey(st.apiKey),
		Source:     st.source,
	}
}

// ConfigUpdate is a persisted-config mutation. Provider/BaseURL/Model always
// overwrite. The API key has three modes: keep (both flags false), set
// (SetAPIKey with APIKey), or clear (ClearAPIKey). Clear wins over Set.
type ConfigUpdate struct {
	Provider    string
	BaseURL     string
	Model       string
	SetAPIKey   bool
	APIKey      string
	ClearAPIKey bool
}

// UpdateConfig persists up for the tenant and hot-swaps the backend. Storing a
// new key requires a secrets key (else [ErrNoSecretsKey]); clearing or keeping
// the key does not.
func (m *Manager) UpdateConfig(ctx context.Context, up ConfigUpdate) error {
	if m.store == nil {
		return ErrNoStore
	}

	existing, err := m.store.Get(ctx, m.tenantID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("aiconfig: load current: %w", err)
	}

	next := StoredConfig{
		TenantID:         m.tenantID,
		Provider:         strings.TrimSpace(up.Provider),
		BaseURL:          strings.TrimSpace(up.BaseURL),
		Model:            strings.TrimSpace(up.Model),
		APIKeyCiphertext: existing.APIKeyCiphertext, // keep by default
	}

	switch {
	case up.ClearAPIKey:
		next.APIKeyCiphertext = nil
	case up.SetAPIKey:
		key := strings.TrimSpace(up.APIKey)
		if key != "" {
			if m.box == nil {
				return ErrNoSecretsKey
			}
			ct, encErr := m.box.EncryptString(key)
			if encErr != nil {
				return fmt.Errorf("aiconfig: encrypt key: %w", encErr)
			}
			next.APIKeyCiphertext = ct
		}
	}

	if _, err := m.store.Set(ctx, next); err != nil {
		return err
	}
	return m.Reload(ctx)
}

// TestResult is the structured outcome of a connection test. It never carries
// key material; Error is scrubbed of the API key and truncated.
type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Error     string `json:"error,omitempty"`
}

const (
	testTimeout      = 10 * time.Second
	testSystemPrompt = "You are a connectivity test. Reply with exactly: OK"
	testUserText     = "ping"
)

// Test runs a minimal Complete to verify connectivity. A non-nil override tests
// that explicit config (as submitted in the request body, plaintext key and
// all); a nil override tests the currently active config. It never returns an
// error value: failures are captured as a scrubbed TestResult.Error.
func (m *Manager) Test(ctx context.Context, override *aibackend.Config) TestResult {
	var provider, baseURL, model, apiKey string
	if override != nil {
		provider, baseURL, model, apiKey = override.Provider, override.BaseURL, override.Model, override.APIKey
	} else {
		st := m.state.Load()
		provider, baseURL, model, apiKey = st.provider, st.baseURL, st.model, st.apiKey
	}

	wire, presetBase := wireFor(provider)
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = presetBase
	}
	client := aibackend.New(aibackend.Config{
		Provider: wire,
		BaseURL:  base,
		APIKey:   apiKey,
		Model:    strings.TrimSpace(model),
	}, m.logger)
	// Label test calls "test" so the connection tester shows up in usage.
	tested := usage.Wrap(client, m.usage, "test")

	tctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	start := time.Now()
	_, err := tested.Complete(tctx, testSystemPrompt, testUserText)
	res := TestResult{
		LatencyMs: time.Since(start).Milliseconds(),
		Provider:  displayProvider(provider),
		Model:     effectiveModel(wire, model),
	}
	if err != nil {
		res.Error = scrubError(err, apiKey)
		return res
	}
	res.OK = true
	return res
}

// hintKey renders a short, non-reversible hint: first three and last three
// runes. Keys of six runes or fewer reveal nothing (they would expose the whole
// value), which is safe because real provider keys are far longer.
func hintKey(key string) string {
	r := []rune(key)
	if len(r) <= 6 {
		if len(r) == 0 {
			return ""
		}
		return "…"
	}
	return string(r[:3]) + "…" + string(r[len(r)-3:])
}

// scrubError renders err for an API response with the API key redacted and the
// message truncated, so an upstream error that echoes an auth header cannot
// leak key material.
func scrubError(err error, apiKey string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if k := strings.TrimSpace(apiKey); k != "" {
		msg = strings.ReplaceAll(msg, k, "***")
	}
	msg = strings.TrimSpace(msg)
	const max = 300
	if len([]rune(msg)) > max {
		msg = string([]rune(msg)[:max]) + "…"
	}
	return msg
}

// displayProvider returns the operator-facing provider id, defaulting an empty
// provider to the wire default (anthropic) that the selector would pick.
func displayProvider(provider string) string {
	if strings.TrimSpace(provider) == "" {
		return aibackend.ProviderAnthropic
	}
	return provider
}

// effectiveModel returns the model that will actually be used: the configured
// one, or the wire client's default.
func effectiveModel(wire, model string) string {
	if strings.TrimSpace(model) != "" {
		return model
	}
	if strings.EqualFold(strings.TrimSpace(wire), aibackend.ProviderOpenAI) {
		return openai.DefaultModel
	}
	return anthropic.DefaultModel
}

// effectiveBase returns the endpoint that will actually be used: the configured
// one, or the wire client's public default.
func effectiveBase(wire, base string) string {
	if strings.TrimSpace(base) != "" {
		return base
	}
	if strings.EqualFold(strings.TrimSpace(wire), aibackend.ProviderOpenAI) {
		return openai.DefaultBaseURL
	}
	return anthropic.DefaultBaseURL
}
