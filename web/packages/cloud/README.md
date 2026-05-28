# @engelos/cloud

**Status:** Skeleton placeholder. Phase 2.

This package will host the SaaS variant served at `app.engelos.com`. It will share UI
with `@engelos/local` via `@engelos/shared`, but adds:

- Billing (Stripe)
- Team management / seats
- Cross-Stream Analytics dashboards
- Multi-tenant routing
- SSR via `adapter-node` or `adapter-vercel`
- Lucia-based auth

For Phase 1 only `@engelos/local` is implemented. Nothing to build here yet.
