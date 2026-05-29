package commands

import "strings"

// ExpandVariables substitutes the three custom-command placeholders in a
// response template:
//
//   - $user    → msg.Username
//   - $channel → msg.Channel
//   - $args    → args joined by single spaces ("" when none)
//
// Substitution is case-sensitive and non-recursive: a $user token in the
// expansion of $args is NOT re-expanded. Unknown $-tokens (e.g. $count,
// $touser) are left untouched so a future extension can take over their
// meaning without breaking stored responses.
//
// The three tokens share no prefix, so a single [strings.Replacer] pass
// handles all of them without longest-match-first ordering concerns.
func ExpandVariables(template string, msg Message, args []string) string {
	if template == "" {
		return ""
	}
	r := strings.NewReplacer(
		"$user", msg.Username,
		"$channel", msg.Channel,
		"$args", strings.Join(args, " "),
	)
	return r.Replace(template)
}
