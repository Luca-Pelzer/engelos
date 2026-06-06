package commands

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
)

// eightBallAnswers is the classic Magic-8-Ball response set (10
// affirmative, 5 non-committal, 5 negative).
var eightBallAnswers = []string{
	"It is certain.",
	"Without a doubt.",
	"Yes definitely.",
	"You may rely on it.",
	"As I see it, yes.",
	"Most likely.",
	"Outlook good.",
	"Yes.",
	"Signs point to yes.",
	"Reply hazy, try again.",
	"Ask again later.",
	"Better not tell you now.",
	"Cannot predict now.",
	"Concentrate and ask again.",
	"Don't count on it.",
	"My reply is no.",
	"My sources say no.",
	"Outlook not so good.",
	"Very doubtful.",
	"It is certain.",
}

// NewEightBallCommand returns "!8ball" (MinRole RoleEveryone, 3s per-user
// cooldown). It answers any question - even an empty one - with a random
// classic Magic-8-Ball reply, e.g. "🎱 @user Most likely.".
func NewEightBallCommand() Command { return newEightBallCommand(rand.Int63) }

// newEightBallCommand is the injectable form: randInt63 supplies a
// non-negative int63-ish value used to index the answer set.
func newEightBallCommand(randInt63 func() int64) Command {
	return Command{
		Name:         "8ball",
		Help:         "Ask the magic 8-ball a question.",
		UserCooldown: 3 * time.Second,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			// Modulo maps the RNG onto the answer index; modulo bias is
			// negligible and acceptable for a chat toy.
			idx := int(randInt63() % int64(len(eightBallAnswers)))
			return Reply{Text: fmt.Sprintf("🎱 %s%s", mentionPrefix(msg), eightBallAnswers[idx])}
		},
	}
}

// NewLurkCommand returns "!lurk" (MinRole RoleEveryone, 10s per-user
// cooldown). It announces that the viewer is now lurking.
func NewLurkCommand() Command {
	return Command{
		Name:         "lurk",
		Help:         "Let chat know you're lurking.",
		UserCooldown: 10 * time.Second,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			return Reply{Text: fmt.Sprintf("%sis now lurking in the shadows 👻 thanks for the support!", mentionPrefix(msg))}
		},
	}
}

// NewUnlurkCommand returns "!unlurk" (MinRole RoleEveryone, 10s per-user
// cooldown). It announces that the viewer is back from lurking.
func NewUnlurkCommand() Command {
	return Command{
		Name:         "unlurk",
		Help:         "Return from lurking.",
		UserCooldown: 10 * time.Second,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			return Reply{Text: fmt.Sprintf("%sis back from the shadows! 👋", mentionPrefix(msg))}
		},
	}
}

// NewDiceCommand returns "!dice" (MinRole RoleEveryone, 3s per-user
// cooldown). It rolls a six-sided die by default; an optional argument
// "!dice <sides>" (2-1000) picks a different die. Invalid/out-of-range
// values fall back to a d6.
func NewDiceCommand() Command { return newDiceCommand(rand.Int63) }

// newDiceCommand is the injectable form: randInt63 supplies a non-negative
// int63-ish value mapped onto [1, sides].
func newDiceCommand(randInt63 func() int64) Command {
	return Command{
		Name:         "dice",
		Help:         "Roll a six-sided die.",
		UserCooldown: 3 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			sides := parseSides(args, 6, 2, 1000)
			// Modulo maps the RNG onto [0, sides); +1 shifts to [1, sides].
			// Modulo bias is acceptable for a chat toy.
			n := int(randInt63()%int64(sides)) + 1
			return Reply{Text: fmt.Sprintf("%srolled a 🎲 %d", mentionPrefix(msg), n)}
		},
	}
}

// NewRollCommand returns "!roll" (MinRole RoleEveryone, 3s per-user
// cooldown). It rolls 1-100 by default ("roll a percentage"); an optional
// argument "!roll <sides>" (2-1000000) widens the range. Invalid/out-of-
// range values fall back to d100.
func NewRollCommand() Command { return newRollCommand(rand.Int63) }

// newRollCommand is the injectable form: randInt63 supplies a non-negative
// int63-ish value mapped onto [1, sides].
func newRollCommand(randInt63 func() int64) Command {
	return Command{
		Name:         "roll",
		Help:         "Roll a number from 1 to a maximum (default 100).",
		UserCooldown: 3 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			sides := parseSides(args, 100, 2, 1000000)
			// Modulo maps the RNG onto [0, sides); +1 shifts to [1, sides].
			// Modulo bias is acceptable for a chat toy.
			n := int(randInt63()%int64(sides)) + 1
			return Reply{Text: fmt.Sprintf("%srolled %d (1-%d)", mentionPrefix(msg), n, sides)}
		},
	}
}

// NewLoveCommand returns "!love" (MinRole RoleEveryone, 5s per-user
// cooldown). "!love <name>" reports a love percentage between the caller
// and the target; with no argument it replies with usage. The percentage
// is derived from an FNV hash of the sorted, lower-cased pair so it is
// STABLE per pair (deterministic UX, and testable without a fake RNG).
func NewLoveCommand() Command {
	return Command{
		Name:         "love",
		Help:         "Measure the love between two users.",
		UserCooldown: 5 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			target := firstTarget(args)
			if target == "" {
				return Reply{Text: fmt.Sprintf("%susage: !love <name>", mentionPrefix(msg))}
			}
			pct := compatibility(msg.Username, target)
			return Reply{Text: fmt.Sprintf("%sloves %s ❤️ %d%%", mentionPrefix(msg), target, pct)}
		},
	}
}

// NewShipCommand returns "!ship" (MinRole RoleEveryone, 5s per-user
// cooldown). "!ship <name1> <name2>" reports a compatibility percentage;
// with fewer than two names it replies with usage. The percentage is
// derived from an FNV hash of the sorted, lower-cased pair so it is STABLE
// per pair (deterministic UX, and testable without a fake RNG).
func NewShipCommand() Command {
	return Command{
		Name:         "ship",
		Help:         "Ship two users together.",
		UserCooldown: 5 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			if len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !ship <name1> <name2>", mentionPrefix(msg))}
			}
			a := strings.TrimPrefix(strings.TrimSpace(args[0]), "@")
			b := strings.TrimPrefix(strings.TrimSpace(args[1]), "@")
			pct := compatibility(a, b)
			return Reply{Text: fmt.Sprintf("💕 %s + %s = %d%% compatible!", a, b, pct)}
		},
	}
}

// NewHugCommand returns "!hug" (MinRole RoleEveryone, 3s per-user
// cooldown). "!hug <name>" hugs the target; with no argument the caller
// hugs themselves.
func NewHugCommand() Command {
	return Command{
		Name:         "hug",
		Help:         "Give someone a hug.",
		UserCooldown: 3 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			target := firstTarget(args)
			if target == "" {
				return Reply{Text: fmt.Sprintf("%shugs themselves 🤗 aww.", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("%shugs %s 🤗", mentionPrefix(msg), target)}
		},
	}
}

// NewSlapCommand returns "!slap" (MinRole RoleEveryone, 3s per-user
// cooldown). "!slap <name>" slaps the target; with no argument it replies
// with usage.
func NewSlapCommand() Command {
	return Command{
		Name:         "slap",
		Help:         "Slap someone with a large trout.",
		UserCooldown: 3 * time.Second,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			target := firstTarget(args)
			if target == "" {
				return Reply{Text: fmt.Sprintf("%susage: !slap <name>", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("%sslaps %s around a bit with a large trout 🐟", mentionPrefix(msg), target)}
		},
	}
}

// firstTarget returns the first argument with a leading "@" stripped, or
// "" when no argument is present.
func firstTarget(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(args[0]), "@")
}

// parseSides reads an optional die-size argument. A missing argument or a
// non-numeric value yields def; a valid number is clamped to [lo, hi].
func parseSides(args []string, def, lo, hi int) int {
	if len(args) == 0 {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// compatibility maps a set of participant names to a stable 0-100
// percentage. Names are lower-cased and sorted before hashing so the
// result is order-independent (love(a,b) == love(b,a)), and an FNV-32a
// hash mod 101 makes the value deterministic per pair - the % never
// changes between calls, which is both nicer UX and trivially testable.
func compatibility(names ...string) int {
	lowered := make([]string, len(names))
	for i, n := range names {
		lowered[i] = strings.ToLower(strings.TrimSpace(n))
	}
	sort.Strings(lowered)
	h := fnv.New32a()
	// Hash.Write never returns an error (documented contract), so the
	// error is intentionally not checked here.
	_, _ = h.Write([]byte(strings.Join(lowered, "\x00")))
	return int(h.Sum32() % 101)
}
