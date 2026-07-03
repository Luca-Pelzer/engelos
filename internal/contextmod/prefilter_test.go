package contextmod

import "testing"

func TestNeedsAI(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", false},
		{"short_clean", "lol", false},
		{"short_gg", "gg wp", false},
		{"emote_spam", "KEKW KEKW KEKW", false},
		{"punctuation_only", "?!?!?!", false},
		{"numbers_only", "12345678", false},
		{"normal_sentence", "das war echt ein klasse spiel heute", true},
		{"url_forces_ai", "lol", true},
		{"url_in_text", "schau mal hier www.botsite.xyz fuer free viewers", true},
		{"discord_invite", "join discord.gg/abcd", true},
		{"twitch_link", "check twitch.tv/someone", true},
		{"spam_marker_short", "folgt mir", true},
		{"f4f", "f4f?", true},
		{"onlyfans", "my onlyfans link in bio", true},
		{"threat_normal_len", "ich finde dich und du wirst es bereuen", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// the url_forces_ai case intentionally reuses a short body plus a URL
			text := c.text
			if c.name == "url_forces_ai" {
				text = "lol http://x.io"
			}
			if got := needsAI(text); got != c.want {
				t.Errorf("needsAI(%q) = %v, want %v", text, got, c.want)
			}
		})
	}
}

func TestNeedsAI_FailSafeOnDoubt(t *testing.T) {
	// A medium-length message with letters and no obvious skip signal must go to
	// the AI (fail-safe: never skip something a human might flag).
	if !needsAI("you should really just stop streaming forever") {
		t.Error("expected fail-safe escalation for a judgeable sentence")
	}
}
