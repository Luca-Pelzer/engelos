// Package overlay serves OBS browser-source overlay pages from an embedded
// asset set under /overlay/{name}. Each overlay is a self-contained HTML
// document that connects to the bot's WebSocket endpoint (/api/v1/ws) and
// renders live stream events as animated, transparent-background graphics
// suitable for compositing in OBS over gameplay footage.
//
// # Why a separate package
//
// internal/web embeds the SvelteKit operator dashboard, which is a single
// app shell with hashed asset routing. The overlay surface is conceptually
// different: each overlay is a standalone page meant to be opened directly
// by OBS, with no shared chrome, no hashed assets and no SPA navigation.
// Keeping it in its own leaf package (no internal/* imports) makes the
// contract obvious and the test surface small.
//
// # Overlays
//
// Three overlays ship today:
//
//   - /overlay/events       — bottom-anchored activity feed (chat + alerts).
//   - /overlay/alerts       — centered single-alert player (subs / raids /
//     streak milestones), Streamlabs-style.
//   - /overlay/leaderboard  — corner panel of top streaks, polled from the
//     REST leaderboard endpoint and refreshed on milestone events.
//
// /overlay/ (empty name) serves a tiny index page listing the three URLs so
// streamers can discover what is available without reading the source.
//
// # Theming
//
// All overlays accept ?theme=dark|light and ?accent=<hex> query parameters.
// Defaults are theme=dark and accent=#a970ff. Each HTML file documents the
// supported parameters in a comment near the top.
//
// # Offline
//
// Overlays use only vanilla CSS and JavaScript: no frameworks, no CDNs, no
// network-dependent fonts. They are designed to render correctly inside
// OBS's embedded Chromium even when the host machine has no internet
// access.
package overlay
