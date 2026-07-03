package aiconfig

import "testing"

func TestProviders_CatalogContainsExpectedEntries(t *testing.T) {
	byID := map[string]ProviderInfo{}
	for _, p := range Providers() {
		byID[p.ID] = p
	}
	for _, id := range []string{ProviderAnthropic, ProviderOpenAI, ProviderGroq, ProviderOllama, ProviderCustom} {
		if _, ok := byID[id]; !ok {
			t.Errorf("catalog missing provider %q", id)
		}
	}
	if got := byID[ProviderAnthropic]; got.DefaultModel != "claude-haiku-4-5" || got.DefaultBaseURL != "https://api.anthropic.com" || got.Wire != "anthropic" || got.Auth != AuthAPIKey {
		t.Errorf("anthropic entry wrong: %+v", got)
	}
	if got := byID[ProviderOpenAI]; got.DefaultModel != "gpt-4o-mini" || got.DefaultBaseURL != "https://api.openai.com" || got.Wire != "openai" {
		t.Errorf("openai entry wrong: %+v", got)
	}
	if got := byID[ProviderGroq]; got.Wire != "openai" || got.DefaultBaseURL != "https://api.groq.com/openai" || got.Auth != AuthAPIKey {
		t.Errorf("groq entry wrong: %+v", got)
	}
	if got := byID[ProviderOllama]; got.Wire != "openai" || got.Auth != AuthNone || got.DefaultBaseURL != "http://localhost:11434" {
		t.Errorf("ollama entry wrong: %+v", got)
	}
	if got := byID[ProviderCustom]; len(got.WireOptions) != 2 {
		t.Errorf("custom entry should offer two wire options: %+v", got)
	}
}

func TestProviders_ReturnsFreshCopy(t *testing.T) {
	a := Providers()
	a[0].Label = "MUTATED"
	if Providers()[0].Label == "MUTATED" {
		t.Fatal("Providers() must return a fresh copy, not the shared catalog")
	}
}

func TestWireFor(t *testing.T) {
	cases := []struct {
		in, wantWire, wantBase string
	}{
		{"anthropic", "anthropic", ""},
		{"openai", "openai", ""},
		{"groq", "openai", groqBaseURL},
		{"GROQ", "openai", groqBaseURL},
		{"ollama", "openai", ollamaBaseURL},
		{" Ollama ", "openai", ollamaBaseURL},
		{"custom", "custom", ""},
		{"", "", ""},
		{"unknown-thing", "unknown-thing", ""},
	}
	for _, c := range cases {
		w, b := wireFor(c.in)
		if w != c.wantWire || b != c.wantBase {
			t.Errorf("wireFor(%q) = (%q, %q); want (%q, %q)", c.in, w, b, c.wantWire, c.wantBase)
		}
	}
}
