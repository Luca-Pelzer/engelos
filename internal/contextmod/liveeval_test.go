package contextmod_test

// Live evaluation harness for the AI escalation path (Phase 1.6).
//
// Opt-in: it only runs with ENGELOS_LIVE_AI=1 and a reachable backend
// configured via the neutral env vars (ENGELOS_AI_PROVIDER / _BASE_URL /
// _API_KEY / _MODEL, legacy fallbacks apply). It sends REAL requests and
// costs real money/tokens - never enable it in CI.
//
//	ENGELOS_LIVE_AI=1 ENGELOS_AI_API_KEY=... go test -count=1 -run TestLiveEval -v ./internal/contextmod
//
// Grading model: every case runs through Classify -> ApplyPolicy exactly like
// production. Cases carry an expectation CLASS, not an exact verdict, because
// model outputs vary:
//
//	allow           must not be enforced AND not audit-flagged as punishable
//	lenient         allow OR audit-only OR plain delete are all acceptable
//	spam_delete     delete without feeding the escalation ladder
//	punish          enforced timeout/delete with ladder, or at minimum
//	                audit-only with a punish action (confidence gate may hold
//	                it back - that is policy working, not a miss)
//
// HARD FAILURE (test fails): any "allow" case that would be ENFORCED
// (timeout/ban, not audit-only). Wrongful punishment is the one unacceptable
// outcome. Everything else is reported as accuracy so regressions are visible
// without flaking the suite.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/aibackend"
	"github.com/Luca-Pelzer/engelos/internal/contextmod"
)

const evalRules = `No harassment, hate speech, threats, doxxing or sexual harassment.
No spam, scams or unwanted self-promotion.
Gaming banter, trash talk between friends and jokes are fine.
Chat is German and English.`

type evalCase struct {
	name   string
	user   string
	text   string
	expect string // allow | lenient | spam_delete | punish
	// history seeds the rolling context window before the reviewed message.
	history []string // "user: text" lines, oldest first
}

func evalCases() []evalCase {
	return []evalCase{
		// --- clearly clean -> allow (enforcement here fails the test) ---
		{name: "clean_en", user: "alice", text: "gg that was a great run", expect: "allow"},
		{name: "clean_de", user: "bob", text: "geiler stream heute, macht spass", expect: "allow"},
		{name: "gaming_kill_slang", user: "carol", text: "ich hab ihn so hart gekillt lol", expect: "allow"},
		{name: "murder_pizza_joke", user: "dave", text: "I'm going to absolutely murder this pizza after the stream", expect: "allow"},
		{name: "hype_caps", user: "erin", text: "LETS GOOO POGGERS", expect: "allow"},
		{name: "self_irony", user: "frank", text: "ich bin so schlecht, ich sollte das game deinstallieren", expect: "allow"},

		// --- hard slang/banter -> lenient (must not be severely punished) ---
		{name: "krebs_gaming_slang", user: "gina", text: "dieses matchmaking ist einfach krebs", expect: "lenient"},
		{name: "git_gud_banter", user: "hank", text: "git gud scrub", expect: "lenient"},
		{name: "bot_insult_mild", user: "iris", text: "du spielst wie ein bot", expect: "lenient"},
		{name: "sarcasm", user: "jay", text: "wow, great play... not", expect: "lenient"},
		{name: "kill_banter_emoji", user: "kim", text: "ich bring dich um wenn du nochmal campst 😂", expect: "lenient"},
		{
			name: "banter_with_context", user: "leo", text: "du bist so ein idiot haha",
			expect: "lenient",
			history: []string{
				"max: hahaha leo du hast schon wieder die bombe fallen lassen",
				"leo: max hat mich reingeschubst der trottel xD",
				"max: das war taktik du lauch :D",
			},
		},

		// --- spam / scams -> delete without ladder ---
		{name: "follower_spam", user: "spam1", text: "🔥 FREE FOLLOWERS at followz .com - click now!!", expect: "spam_delete"},
		{name: "follow4follow", user: "spam2", text: "check out my channel twitch.tv/xyz follow for follow", expect: "spam_delete"},
		{name: "crypto_scam", user: "spam3", text: "Elon is giving away BTC, DM me to double your coins fast", expect: "spam_delete"},
		{name: "phishing_link", user: "spam4", text: "your account will be suspended, verify now: bit.ly/tw-verify", expect: "spam_delete"},

		// --- real abuse -> punish ---
		{name: "targeted_harassment", user: "troll1", text: "dave you are worthless, everyone in this chat hates you", expect: "punish"},
		{name: "kys_targeted", user: "troll2", text: "kys dave, nobody would miss you", expect: "punish"},
		{name: "kys_spaced_evasion", user: "troll3", text: "k y s d a v e", expect: "punish"},
		{name: "threat", user: "troll4", text: "I know where you live dave, watch your back after the stream", expect: "punish"},
		{name: "doxx_attempt", user: "troll5", text: "daves echter name ist D. Meyer und er wohnt in der Hauptstr. 5 in Koeln", expect: "punish"},
		{name: "dehumanizing_hate", user: "troll6", text: "all of them are subhuman vermin and should be gassed", expect: "punish"},
		{name: "sexism", user: "troll7", text: "girls can't play shooters, go back to the kitchen where you belong", expect: "punish"},
		{name: "sexual_harassment", user: "troll8", text: "send me pics or I'll make your life here hell", expect: "punish"},
		{name: "foreign_lang_abuse", user: "troll9", text: "eres basura, matate ya", expect: "punish"},
		{
			name: "escalating_with_context", user: "troll10", text: "und du bist immer noch ein opfer",
			expect: "punish",
			history: []string{
				"troll10: dave du bist der letzte muell",
				"dave: lass mich einfach in ruhe",
				"troll10: heul doch, alle finden dich peinlich",
			},
		},
	}
}

func TestLiveEval(t *testing.T) {
	if os.Getenv("ENGELOS_LIVE_AI") != "1" {
		t.Skip("live eval disabled; set ENGELOS_LIVE_AI=1 (sends real, billable AI requests)")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	backend := aibackend.New(aibackend.LoadConfig(), logger)

	opts := contextmod.DefaultOptions()
	opts.GlobalPerSecond = 4
	opts.DedupTTLSeconds = 0 // distinct cases anyway; keep the run honest
	esc := contextmod.NewEscalator(backend, opts)
	policy := contextmod.DefaultPolicy()

	type row struct {
		c       evalCase
		d       contextmod.Decision
		o       contextmod.PolicyOutcome
		pass    bool
		verdict string
	}

	var (
		rows      []row
		passCount int
		hardFails []string
	)

	for i, c := range evalCases() {
		channel := fmt.Sprintf("eval-%02d", i)
		for _, h := range c.history {
			user, text, ok := strings.Cut(h, ": ")
			if !ok {
				t.Fatalf("bad history line %q", h)
			}
			esc.Observe(channel, user, text)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		d := esc.ClassifyInChannel(ctx, channel, evalRules, c.user, c.text)
		cancel()
		o := contextmod.ApplyPolicy(d, policy)

		enforced := !o.AuditOnly && o.Action != contextmod.VerdictAllow
		punished := o.Action == contextmod.VerdictTimeout ||
			(o.Action == contextmod.VerdictDelete && o.FeedLadder)

		var pass bool
		switch c.expect {
		case "allow":
			pass = o.Action == contextmod.VerdictAllow && !o.AuditOnly
			if enforced {
				hardFails = append(hardFails,
					fmt.Sprintf("%s: clean message would be enforced (%s, timeout=%s)", c.name, o.Action, o.Timeout))
			}
		case "lenient":
			// Anything except an enforced timeout/ban is acceptable.
			pass = !(enforced && o.Action == contextmod.VerdictTimeout)
		case "spam_delete":
			pass = o.Action == contextmod.VerdictDelete && !o.FeedLadder
		case "punish":
			// Enforced punishment, or an audit-only punish verdict held back by
			// the confidence gate (policy working as designed).
			pass = punished || (o.AuditOnly && d.Action != contextmod.VerdictAllow && d.Verdict != contextmod.VerdictUnknown)
		default:
			t.Fatalf("unknown expect class %q", c.expect)
		}
		if pass {
			passCount++
		}
		rows = append(rows, row{c: c, d: d, o: o, pass: pass})
	}

	// Report table.
	t.Logf("%-24s %-8s %-10s %-11s %3s %5s  %-6s %-5s %s",
		"CASE", "EXPECT", "VERDICT", "CATEGORY", "SEV", "CONF", "POLICY", "PASS", "REASON")
	for _, r := range rows {
		policyStr := string(r.o.Action)
		if r.o.AuditOnly {
			policyStr += "/audit"
		}
		if r.o.FeedLadder {
			policyStr += "+ladder"
		}
		mark := "ok"
		if !r.pass {
			mark = "MISS"
		}
		t.Logf("%-24s %-8s %-10s %-11s %3d %5.2f  %-12s %-5s %.60s",
			r.c.name, r.c.expect, r.d.Verdict, r.d.Category, r.d.Severity, r.d.Confidence, policyStr, mark, r.d.Reason)
	}
	t.Logf("ACCURACY: %d/%d (%.0f%%)", passCount, len(rows), 100*float64(passCount)/float64(len(rows)))

	if len(hardFails) > 0 {
		t.Fatalf("HARD FAIL - clean messages would be wrongfully enforced:\n%s", strings.Join(hardFails, "\n"))
	}
}
