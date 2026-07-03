package plugins

// builtin is a manifest-only plugin: the restart-based model needs no behaviour
// hook because store-opening and route/command wiring live in cmd/engelos,
// gated on the resolved enabled state.
type builtin struct{ manifest Manifest }

func (b builtin) Manifest() Manifest { return b.manifest }

// Quotes is the pilot legacy plugin: the per-channel quote book and its
// !quote/!addquote/!delquote chat commands. It defaults on, preserving the
// pre-plugin behaviour where the quotes store was always opened, so an untouched
// deployment behaves identically until an operator disables it.
func Quotes() Plugin {
	return builtin{manifest: Manifest{
		ID:             "quotes",
		Name:           "Quotes",
		Description:    "Per-channel quote book with !quote, !addquote and !delquote chat commands and a dashboard editor.",
		Tier:           TierLegacy,
		SettingsHref:   "/quotes",
		DefaultEnabled: true,
	}}
}

// Counters is the named-counter legacy plugin: the per-channel counter book, its
// !counter/!counter+/!counter-/!setcounter/!resetcounter chat commands and the
// Channel-Points counter_increment/counter_reset actions. It defaults on,
// preserving today's always-opened counter store.
func Counters() Plugin {
	return builtin{manifest: Manifest{
		ID:             "counters",
		Name:           "Counters",
		Description:    "Named per-channel counters with !counter chat commands and Channel-Points counter actions.",
		Tier:           TierLegacy,
		SettingsHref:   "/counters",
		DefaultEnabled: true,
	}}
}

// Loyalty is the points-economy legacy plugin: the loyalty store, its
// leaderboard/adjust endpoints and its !points/!give chat commands. The
// points-economy games (gamble, slots, duel, heist, redeem) and per-message
// earning depend on it, so disabling loyalty also disables those. It defaults
// on, preserving today's always-opened loyalty store.
func Loyalty() Plugin {
	return builtin{manifest: Manifest{
		ID:             "loyalty",
		Name:           "Loyalty points",
		Description:    "Channel points economy: !points/!give plus the games (gamble, slots, duel, heist, redeem) that draw on the same balance.",
		Tier:           TierLegacy,
		SettingsHref:   "/loyalty",
		DefaultEnabled: true,
	}}
}

// Pity is the pity-system template plugin: per-message pity points, the !pity
// command, the pity leaderboard (shared with streak) and the /api/v1/pity
// endpoints. It defaults on, preserving today's always-constructed pity system.
func Pity() Plugin {
	return builtin{manifest: Manifest{
		ID:             "pity",
		Name:           "Pity system",
		Description:    "Gacha-style pity points that accrue per chat message, with !pity, a leaderboard and grant/roll controls.",
		Tier:           TierTemplate,
		SettingsHref:   "/pity",
		DefaultEnabled: true,
	}}
}

// Streak is the streak-system template plugin: per-message streak ticking, the
// !streak command, the streak leaderboard (shared with pity) and the
// /api/v1/streak endpoints. It defaults on, preserving today's
// always-constructed streak system.
func Streak() Plugin {
	return builtin{manifest: Manifest{
		ID:             "streak",
		Name:           "Streak system",
		Description:    "Daily watch streaks that tick per chat message, with !streak, a leaderboard, freezes and milestone alerts.",
		Tier:           TierTemplate,
		SettingsHref:   "/streak",
		DefaultEnabled: true,
	}}
}

// Liveops is the event-schedule template plugin: the per-channel event plan, its
// !nextevent/!schedule/!addevent/!delevent chat commands and the /api/v1/liveops
// endpoints. It defaults on, preserving today's always-opened liveops store.
func Liveops() Plugin {
	return builtin{manifest: Manifest{
		ID:             "liveops",
		Name:           "Event templates",
		Description:    "Scheduled stream events with !nextevent/!schedule/!addevent/!delevent and a dashboard planner.",
		Tier:           TierTemplate,
		SettingsHref:   "/liveops",
		DefaultEnabled: true,
	}}
}

// Wrapped is the recap template plugin: the per-viewer Stream-Wrapped stats
// recorded from chat/sub/raid events and served at the public /api/v1/wrapped
// card. It defaults on, preserving today's always-opened wrapped store; disabled
// stops the dispatcher recording and unmounts the card.
func Wrapped() Plugin {
	return builtin{manifest: Manifest{
		ID:             "wrapped",
		Name:           "Recap template",
		Description:    "Stream-Wrapped viewer recap cards built from message, subscription and raid activity.",
		Tier:           TierTemplate,
		SettingsHref:   "/wrapped",
		DefaultEnabled: true,
	}}
}

// Moments is the shared-moment template plugin: the !moment/!here chat commands
// and the /api/v1/moments endpoints for time-boxed viewer check-ins. It defaults
// on, preserving today's always-opened moments store.
func Moments() Plugin {
	return builtin{manifest: Manifest{
		ID:             "moments",
		Name:           "Moment template",
		Description:    "Time-boxed shared moments viewers join with !here, opened via !moment and shown on overlays.",
		Tier:           TierTemplate,
		SettingsHref:   "/moments",
		DefaultEnabled: true,
	}}
}

// Songrequests is the music legacy plugin (RES-19 "Music Plugin"). Unlike every
// other builtin it defaults OFF, preserving the RES-19 quarantine. The plugin
// toggle is the source of truth, but the legacy ENGELOS_FEATURE_SONGREQUESTS env
// flag still forces it on for backward compatibility, so a box that sets the
// flag behaves exactly as before while a box without it stays default-off.
func Songrequests() Plugin {
	return builtin{manifest: Manifest{
		ID:             "songrequests",
		Name:           "Music Plugin",
		Description:    "Spotify/YouTube song requests with !sr/!song/!skipsong and a now-playing overlay. Default off (RES-19); ENGELOS_FEATURE_SONGREQUESTS forces it on.",
		Tier:           TierLegacy,
		SettingsHref:   "/songrequests",
		DefaultEnabled: false,
	}}
}
