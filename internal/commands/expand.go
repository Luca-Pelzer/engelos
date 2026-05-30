package commands

import (
	"math/rand"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ExpandVariables expands a custom-command response template into its final
// reply text. It keeps the legacy bare tokens working and adds a
// Nightbot/StreamElements-style $(...) variable system on top.
//
// Legacy bare tokens (expanded FIRST, for backward compatibility with
// already-stored commands):
//
//   - $user    → msg.Username
//   - $channel → msg.Channel (verbatim)
//   - $args    → args joined by single spaces
//
// $(...) function-style variables (expanded SECOND):
//
//   - $(user)              → msg.Username
//   - $(channel)           → msg.Channel with a leading "#" stripped
//   - $(args)              → all args joined by single spaces
//   - $(touser)            → first arg with a leading "@" stripped; falls
//     back to msg.Username when there are no args
//   - $(1) … $(9)          → the Nth argument, 1-indexed; "" if absent
//   - $(count)             → an injected counter value; "" when no counter
//     is wired (the default path used by this function)
//   - $(random a b)        → a pseudo-random integer in [a,b] inclusive
//     (alias: $(random.number a b)); "" on parse error or a>b
//   - $(random.pick x y …) → one of the space-separated options at random;
//     "" when there are no options
//   - $(math expr)         → integer arithmetic over + - * / and parens,
//     e.g. $(math 1 + 2 * 3) → "7"; "" on parse error or divide-by-zero
//   - $(time)              → current local time as "15:04 MST"
//
// Out-of-scope variables ($(eval …), $(urlfetch …), $(uptime …)) and any
// other unrecognised name expand to the empty string "" (Nightbot
// behaviour) rather than being left literal.
//
// Expansion is NON-RECURSIVE: $(...) variables are expanded in a single
// left-to-right pass and the output of one variable is never re-scanned for
// further variables. This is a deliberate security choice - it prevents a
// user from typing a $(...) whose expansion injects another $(...).
//
// Escaping: a literal "$(" can be written as "\$(" - a backslash before a
// "$" both suppresses variable expansion at that position and is itself
// removed, so "\$(user)" renders as the literal "$(user)".
//
// Substitution is case-sensitive. Malformed input never panics; it yields a
// best-effort string.
func ExpandVariables(template string, msg Message, args []string) string {
	return expandWith(template, msg, args, defaultExpandDeps())
}

// expandDeps holds the injectable, side-effecting dependencies so tests can
// assert exact output. The public ExpandVariables wires the real ones via
// defaultExpandDeps; expandWith stays unexported and deterministic-testable.
type expandDeps struct {
	now      func() time.Time   // clock for $(time)
	randIntN func(n int) int    // returns a value in [0,n) for the RNG vars
	counter  func() (int, bool) // $(count) value; ok=false => $(count) is ""
}

// defaultExpandDeps returns production dependencies: the real clock, the
// shared math/rand source, and no counter (so $(count) is empty on the
// legacy path until a richer entry point wires one in).
func defaultExpandDeps() expandDeps {
	return expandDeps{
		now:      time.Now,
		randIntN: rand.Intn,
		counter:  nil,
	}
}

// expandWith is the testable core of ExpandVariables. It expands the legacy
// bare tokens first (via a single Replacer pass) and then the $(...)
// function variables, using deps for all non-deterministic behaviour.
func expandWith(template string, msg Message, args []string, deps expandDeps) string {
	if template == "" {
		return ""
	}
	legacy := strings.NewReplacer(
		"$user", msg.Username,
		"$channel", msg.Channel,
		"$args", strings.Join(args, " "),
	)
	return expandFuncVars(legacy.Replace(template), msg, args, deps)
}

// expandFuncVars performs the single left-to-right, non-recursive $(...)
// pass. A "\$" sequence is unescaped to "$" in place, which both removes
// the backslash and prevents the following "$(" from being treated as a
// variable. Resolved values are written verbatim and never re-scanned.
func expandFuncVars(s string, msg Message, args []string, deps expandDeps) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '(' {
			closeIdx := matchParen(s, i+1)
			if closeIdx < 0 {
				// Unmatched "$(" - emit best-effort and keep scanning.
				b.WriteByte(s[i])
				i++
				continue
			}
			b.WriteString(deps.resolveVar(s[i+2:closeIdx], msg, args))
			i = closeIdx + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// matchParen returns the index of the ')' matching the '(' at index open,
// tracking nesting depth so $(math (4+5)/3) captures its inner parens
// correctly. It returns -1 when no matching ')' exists.
func matchParen(s string, open int) int {
	depth := 0
	for j := open; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// resolveVar maps one captured $(...) inner string to its value. The name is
// the token up to the first whitespace (dotted suffixes like random.number
// are part of the name); the remainder is the raw argument string. Unknown
// names - including the out-of-scope eval/urlfetch/uptime - return "".
func (d expandDeps) resolveVar(inner string, msg Message, args []string) string {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return ""
	}
	name, rest := inner, ""
	if sp := strings.IndexFunc(inner, unicode.IsSpace); sp >= 0 {
		name = inner[:sp]
		rest = strings.TrimSpace(inner[sp:])
	}

	switch name {
	case "user":
		return msg.Username
	case "channel":
		return strings.TrimPrefix(msg.Channel, "#")
	case "args":
		return strings.Join(args, " ")
	case "touser":
		if len(args) > 0 {
			return strings.TrimPrefix(args[0], "@")
		}
		return msg.Username
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(name[0] - '0')
		if idx >= 1 && idx <= len(args) {
			return args[idx-1]
		}
		return ""
	case "count":
		if d.counter != nil {
			if v, ok := d.counter(); ok {
				return strconv.Itoa(v)
			}
		}
		return ""
	case "random", "random.number":
		return randomRange(rest, d.randIntN)
	case "random.pick":
		return randomPick(rest, d.randIntN)
	case "math":
		return evalMath(rest)
	case "time":
		return d.now().Format("15:04 MST")
	default:
		return ""
	}
}

// randomRange parses "a b" as two ints and returns a random integer in
// [a,b] inclusive via randIntN. It returns "" on a missing/extra/invalid
// operand or when a > b.
func randomRange(rest string, randIntN func(int) int) string {
	f := strings.Fields(rest)
	if len(f) != 2 {
		return ""
	}
	a, e1 := strconv.Atoi(f[0])
	b, e2 := strconv.Atoi(f[1])
	if e1 != nil || e2 != nil || a > b {
		return ""
	}
	return strconv.Itoa(a + randIntN(b-a+1))
}

// randomPick returns one of the space-separated options at random via
// randIntN, or "" when there are no options.
func randomPick(rest string, randIntN func(int) int) string {
	opts := strings.Fields(rest)
	if len(opts) == 0 {
		return ""
	}
	return opts[randIntN(len(opts))]
}

// evalMath evaluates a simple integer arithmetic expression and returns its
// decimal string. It is a hand-written recursive-descent parser over + - * /
// and parentheses (NOT a general code evaluator) so untrusted command
// templates cannot execute arbitrary logic. Division is integer/truncating;
// a parse error, leftover input, or divide-by-zero yields "".
func evalMath(expr string) string {
	p := &mathParser{s: expr}
	v := p.parseExpr()
	p.skipSpace()
	if p.err || p.pos != len(p.s) {
		return ""
	}
	return strconv.Itoa(v)
}

// mathParser is the state for evalMath's recursive-descent evaluation.
// Grammar:
//
//	expr   := term (('+'|'-') term)*
//	term   := factor (('*'|'/') factor)*
//	factor := ('+'|'-') factor | '(' expr ')' | digits
type mathParser struct {
	s   string
	pos int
	err bool
}

func (p *mathParser) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *mathParser) parseExpr() int {
	v := p.parseTerm()
	for {
		p.skipSpace()
		if p.err || p.pos >= len(p.s) {
			return v
		}
		op := p.s[p.pos]
		if op != '+' && op != '-' {
			return v
		}
		p.pos++
		r := p.parseTerm()
		if op == '+' {
			v += r
		} else {
			v -= r
		}
	}
}

func (p *mathParser) parseTerm() int {
	v := p.parseFactor()
	for {
		p.skipSpace()
		if p.err || p.pos >= len(p.s) {
			return v
		}
		op := p.s[p.pos]
		if op != '*' && op != '/' {
			return v
		}
		p.pos++
		r := p.parseFactor()
		if p.err {
			return v
		}
		if op == '*' {
			v *= r
		} else {
			if r == 0 {
				p.err = true
				return v
			}
			v /= r
		}
	}
}

func (p *mathParser) parseFactor() int {
	p.skipSpace()
	if p.pos >= len(p.s) {
		p.err = true
		return 0
	}
	switch c := p.s[p.pos]; {
	case c == '-':
		p.pos++
		return -p.parseFactor()
	case c == '+':
		p.pos++
		return p.parseFactor()
	case c == '(':
		p.pos++
		v := p.parseExpr()
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			p.err = true
			return 0
		}
		p.pos++
		return v
	}
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		p.err = true
		return 0
	}
	n, err := strconv.Atoi(p.s[start:p.pos])
	if err != nil {
		p.err = true
		return 0
	}
	return n
}
