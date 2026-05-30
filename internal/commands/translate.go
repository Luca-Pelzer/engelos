package commands

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// TranslateConfigStore is the narrow management surface the !translate built-in
// needs to read and change the per-channel chat-translation settings. An
// adapter over [github.com/Luca-Pelzer/engelos/internal/translate.Store] is
// wired in main; this interface lives HERE so internal/commands does NOT import
// internal/translate (mirrors [FeatureToggleStore]).
//
// TranslateStatus reports whether translation is on and the target language
// code; SetTranslateEnabled flips the channel switch; SetTranslateLang persists
// a new target language. main's adapter chooses the tenant_id and normalises
// the channel.
type TranslateConfigStore interface {
	TranslateStatus(ctx context.Context, channel string) (enabled bool, targetLang string)
	SetTranslateEnabled(ctx context.Context, channel string, enabled bool) error
	SetTranslateLang(ctx context.Context, channel, lang string) error
}

// translateLangRE validates a target language code argument as an ISO 639-1
// style code with an optional region suffix, for example "en" or "pt-br".
var translateLangRE = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2,4})?$`)

// NewTranslateToggleCommand returns "!translate". Mods-only.
//
// Usage: "!translate" or "!translate status" reports the current state;
// "!translate on" / "!translate off" flips per-channel chat translation;
// "!translate lang <code>" sets the target language (for example "en" or "es").
// A nil store yields a one-line "translation is unavailable" reply.
func NewTranslateToggleCommand(store TranslateConfigStore) Command {
	return Command{
		Name:         "translate",
		Help:         "Turn chat translation on or off and set the language - !translate on|off|status|lang <code>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%stranslation is unavailable",
					mentionPrefix(msg))}
			}
			action := "status"
			if len(args) > 0 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}
			switch action {
			case "status", "":
				enabled, lang := store.TranslateStatus(ctx, msg.Channel)
				return Reply{Text: fmt.Sprintf("%schat translation is %s (target: %s)",
					mentionPrefix(msg), onOff(enabled), lang)}
			case "on", "enable", "enabled":
				if err := store.SetTranslateEnabled(ctx, msg.Channel, true); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the translation setting",
						mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%schat translation is now ON ✅", mentionPrefix(msg))}
			case "off", "disable", "disabled":
				if err := store.SetTranslateEnabled(ctx, msg.Channel, false); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the translation setting",
						mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%schat translation is now OFF 🚫", mentionPrefix(msg))}
			case "lang", "language":
				if len(args) < 2 {
					return Reply{Text: fmt.Sprintf("%susage: !translate lang <code> (e.g. en, es, de)",
						mentionPrefix(msg))}
				}
				lang := strings.ToLower(strings.TrimSpace(args[1]))
				if !translateLangRE.MatchString(lang) {
					return Reply{Text: fmt.Sprintf("%s%q is not a valid language code (use e.g. en, es, pt-br)",
						mentionPrefix(msg), lang)}
				}
				if err := store.SetTranslateLang(ctx, msg.Channel, lang); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the translation language",
						mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%stranslation target language is now %s", mentionPrefix(msg), lang)}
			default:
				return Reply{Text: fmt.Sprintf("%susage: !translate on|off|status|lang <code>",
					mentionPrefix(msg))}
			}
		},
	}
}
