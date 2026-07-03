# AI-Mod Live Evaluation — 2026-07-02

First full live evaluation of the AI escalation path (`internal/contextmod`)
after the Phase-1 build-out: pluggable provider backend, rolling per-channel
chat-context window, category/severity/confidence policy, and audit-only
confidence gating.

## Setup

| Item | Value |
|---|---|
| Harness | `internal/contextmod/liveeval_test.go` (`TestLiveEval`, opt-in via `ENGELOS_LIVE_AI=1`) |
| Backend | Anthropic wire, `claude-haiku-4-5`, BYOK endpoint via neutral env vars |
| Pipeline under test | `Escalator.ClassifyInChannel` → `ApplyPolicy(DefaultPolicy())` — identical to production |
| Policy defaults | confidence gate 0.6, spam → delete without ladder, severity drives timeouts |
| Cases | 26 synthetic messages (see below), 2 of them with seeded chat history to exercise the context window |

All case texts are **synthetic test data** written for this evaluation; the
abusive examples exist solely to verify the moderator catches them.

## Grading model

Cases are graded by expectation class, not exact verdict:

- **allow** — must not be enforced; an enforced action here fails the whole run (wrongful punishment is the one unacceptable outcome)
- **lenient** — allow, audit-only, or plain delete all acceptable (hard slang/banter cases)
- **spam_delete** — delete without feeding the repeat-offender ladder
- **punish** — enforced timeout with ladder, or an audit-only punish verdict held back by the confidence gate

## Result: 26/26 (100%)

| Class | Cases | Passed | Notes |
|---|---|---|---|
| allow (clean) | 6 | 6 | incl. "murder this pizza", hype caps, self-irony — zero false positives |
| lenient (hard slang/banter) | 6 | 6 | all six resolved to plain **allow**, incl. German gaming slang ("krebs"), kill-banter with emoji, and insult-between-friends **with friendly seeded history** |
| spam_delete | 4 | 4 | follower spam, follow4follow, crypto scam, phishing link — all delete, none fed the ladder; phishing rated severity 2 |
| punish | 10 | 10 | targeted harassment, "kys" (plain **and** spaced evasion "k y s"), threat, doxxing, dehumanizing hate, sexism, sexual coercion, Spanish-language abuse, and escalation-only-visible-in-context |

### Context-window proof (Phase 1.3 working as designed)

The same shape of message flipped verdicts based on seeded history:

- `"du bist so ein idiot haha"` after three lines of mutual friendly banter → **allow** ("friendly gaming banter between friends, joking insult in context")
- `"und du bist immer noch ein opfer"` after three lines of one-sided bullying → **timeout + ladder**, category harassment, severity 2

Single-turn classification cannot make this distinction; the rolling window is
what makes it possible.

### Verdict quality observations

- Confidence was well-calibrated: 0.95–0.99 on clear cases, 0.85 on the two
  genuinely contextual harassment cases — comfortably above the 0.6 gate, so
  everything that should enforce would enforce.
- Category labels were sensible throughout (spam/harassment/threat/doxxing/
  hate/sexism); "kys" variants land as harassment/threat with severity 3.
- Reasons are short, human-readable and audit-friendly.

## Caveats / next step

This is a curated-case eval, not live traffic. The remaining Phase-1.6 step is
the **shadow-mode soak**: deploy to the live bot, run dry-run during at least
one full stream, and review the audit log (now carrying category/severity/
confidence per row) for false-positive patterns on real chat before any
enforcement is enabled.

## Reproduce

```bash
ENGELOS_LIVE_AI=1 \
ENGELOS_AI_PROVIDER=anthropic \
ENGELOS_AI_API_KEY=... \
ENGELOS_AI_MODEL=claude-haiku-4-5 \
go test -count=1 -run TestLiveEval -v ./internal/contextmod
```

Sends real, billable requests (26 calls, ~45s at the default 4 req/s limit).
