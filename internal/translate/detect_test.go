package translate

import "testing"

func TestAlreadyInTargetLang_NonLatinScripts(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		target string
		want   bool
	}{
		{"russian to en", "Привет, как дела сегодня?", "en", false},
		{"russian to ru", "Привет, как дела сегодня?", "ru", true},
		{"chinese to en", "你好今天过得怎么样", "en", false},
		{"chinese to zh", "你好今天过得怎么样", "zh", true},
		{"japanese kana to ja", "こんにちは、げんきですか", "ja", true},
		{"korean to ko", "안녕하세요 오늘 어떻게 지내세요", "ko", true},
		{"greek to el", "Γεια σου τι κανεις σημερα", "el", true},
		{"greek to en", "Γεια σου τι κανεις σημερα", "en", false},
		{"arabic to en", "مرحبا كيف حالك اليوم", "en", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AlreadyInTargetLang(tc.text, tc.target); got != tc.want {
				t.Fatalf("AlreadyInTargetLang(%q,%q)=%v want %v", tc.text, tc.target, got, tc.want)
			}
		})
	}
}

func TestAlreadyInTargetLang_LatinReliable(t *testing.T) {
	// Long, clearly-Polish text: reliably detected, so target=pl is "already".
	pl := "Cześć, jak się masz dzisiaj? Mam nadzieję, że wszystko u ciebie w porządku i dobrze się bawisz."
	if !AlreadyInTargetLang(pl, "pl") {
		t.Errorf("expected reliable Polish to count as already-Polish")
	}
	// The same Polish text is NOT English, so it must be translated.
	if AlreadyInTargetLang(pl, "en") {
		t.Errorf("Polish text must not count as already-English")
	}
}

func TestAlreadyInTargetLang_Conservative(t *testing.T) {
	// Empty/whitespace is a no-op: treated as already-there so callers skip.
	if !AlreadyInTargetLang("   ", "en") {
		t.Errorf("blank text should be treated as already-target")
	}
	// Short, ambiguous Latin text is unreliable -> must translate (false).
	if AlreadyInTargetLang("ok", "en") {
		t.Errorf("ambiguous short text should not be confidently already-target")
	}
}

func TestNormalizeLangCode(t *testing.T) {
	cases := map[string]string{
		"EN":      "en",
		"en-US":   "en",
		" pt-BR ": "pt",
		"de":      "de",
	}
	for in, want := range cases {
		if got := normalizeLangCode(in); got != want {
			t.Errorf("normalizeLangCode(%q)=%q want %q", in, got, want)
		}
	}
}
