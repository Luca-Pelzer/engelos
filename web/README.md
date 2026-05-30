# engelOS - Web

> *The streaming bot that remembers you. Open source. Run it anywhere.*

This is the frontend monorepo for engelOS. It ships **two** SvelteKit applications
sharing one component library:

| Package | Purpose | Adapter |
|---|---|---|
| `@engelos/shared` | UI components, API client, WebSocket client, design tokens | (library) |
| `@engelos/local` | Self-hosted variant - embedded as static files in the Go binary | `adapter-static` |
| `@engelos/cloud` | SaaS variant for `app.engelos.com` (skeleton only - Phase 2) | TBD |

## Stack

- **Svelte 5** with runes (`$state`, `$derived`, `$effect`)
- **SvelteKit 2**
- **Tailwind CSS 4** (`@import "tailwindcss"` + `@theme`)
- **TypeScript** (strict)
- **Vite 6**

## Develop

```bash
pnpm install
pnpm dev              # → http://localhost:5173, proxies /api → :8080
```

## Build (self-hosted variant → embeddable static)

```bash
pnpm build
# Output: packages/local/build/
# Go daemon embeds this via //go:embed
```

## Design

Dark-by-default. Violet/purple accent (`#8b5cf6`). Inter for UI, JetBrains Mono for IDs.
Aesthetic reference points: **Linear** (sharpness), **Vercel** (clarity), **Discord**
(community-warmth), **Raycast** (snappiness). Built from scratch with Tailwind utilities,
no heavy component library.

## License

AGPL-3.0-or-later (matches the engelOS core daemon).
