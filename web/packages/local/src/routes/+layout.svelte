<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { Sidebar, TopBar } from '@engelos/shared/components';
  import { ws, toasts, auth, ApiException } from '@engelos/shared/lib';

  let { children } = $props();

  const path = $derived($page.url.pathname);

  // Public routes render without an auth check. Everything else is the
  // owner-only dashboard and must not render until /users/me confirms a
  // session; otherwise an anonymous visitor briefly sees real data.
  const isPublic = $derived(
    path === '/login' || path === '/setup' || path.startsWith('/setup/'),
  );

  const isChromeless = $derived(isPublic);

  // 'checking' until /users/me resolves, then 'in' or 'out'. The dashboard
  // tree is gated on 'in' so there is no flash of authed content for anon
  // users, and the redirect to /login only fires once, avoiding loops.
  let authState = $state<'checking' | 'in' | 'out'>('checking');

  async function verifySession() {
    if (isPublic) {
      authState = 'in';
      return;
    }
    try {
      await auth.me();
      authState = 'in';
    } catch (err) {
      authState = 'out';
      if (err instanceof ApiException && err.status === 401) {
        void goto('/login');
      }
    }
  }

  const pageTitle = $derived.by(() => {
    // Titles mirror the sidebar IA: a pillar/section plus the page name.
    // Routes are grouped by product area (AI-Mod, Workflows, Integrations,
    // Runtime) so the top bar reinforces the navigation structure.
    if (path === '/')              return 'Dashboard';
    // AI-Mod — one pillar, three coordinated views.
    if (path.startsWith('/ai-mod'))      return 'AI-Mod';
    if (path.startsWith('/contextmod'))   return 'AI-Mod · AI Escalation';
    if (path.startsWith('/automod'))      return 'AI-Mod · Fast Path';
    // Workflows
    if (path.startsWith('/workflows'))    return 'Workflows';
    if (path.startsWith('/actions'))      return 'Workflows · Builder';
    if (path.startsWith('/commands'))     return 'Workflows · Command Triggers';
    if (path.startsWith('/timers'))       return 'Workflows · Schedule Triggers';
    if (path.startsWith('/redemptions'))  return 'Workflows · Redemption Triggers';
    if (path.startsWith('/rewards'))      return 'Workflows · Twitch Rewards';
    if (path.startsWith('/counters'))     return 'Workflows · Counter Actions';
    // Integrations
    if (path.startsWith('/integrations')) return 'Integrations';
    if (path.startsWith('/tts'))          return 'Integrations · AI Voice';
    if (path.startsWith('/cohost'))       return 'Integrations · AI Co-Host';
    if (path.startsWith('/translate'))    return 'Integrations · Translate';
    if (path.startsWith('/clipper'))      return 'Integrations · Clip Workflow';
    if (path.startsWith('/songrequests')) return 'Integrations · Music Plugin';
    if (path.startsWith('/loyalty'))      return 'Integrations · Loyalty Plugin';
    if (path.startsWith('/pity'))         return 'Integrations · Pity Template';
    if (path.startsWith('/streak'))       return 'Integrations · Streak Template';
    if (path.startsWith('/moments'))      return 'Integrations · Moment Template';
    if (path.startsWith('/wrapped'))      return 'Integrations · Recap Template';
    if (path.startsWith('/quotes'))       return 'Integrations · Quote Plugin';
    if (path.startsWith('/liveops'))      return 'Integrations · Event Templates';
    // Runtime
    if (path.startsWith('/chat'))         return 'Runtime · Live Chat';
    if (path.startsWith('/connections'))  return 'Runtime · Connections';
    if (path.startsWith('/members'))      return 'Runtime · Members';
    if (path.startsWith('/import'))       return 'Runtime · Import';
    // Settings / account
    if (path.startsWith('/settings'))     return 'Settings';
    if (path.startsWith('/upgrade'))      return 'Upgrade to Cloud';
    return '';
  });

  onMount(() => {
    void verifySession();
  });

  // Connect the live WebSocket only once a session is confirmed, since the
  // /ws endpoint is now owner-gated and an anonymous connect would just fail.
  $effect(() => {
    if (authState === 'in' && !isPublic) {
      ws.connect();
      return () => ws.disconnect();
    }
  });
</script>

{#if isChromeless}
  <main class="min-h-screen">
    {@render children()}
  </main>
{:else if authState === 'in'}
  <div class="scene" aria-hidden="true">
    <div class="orb a"></div>
    <div class="orb b"></div>
    <div class="orb c"></div>
  </div>
  <div class="app">
    <Sidebar current={path} />
    <div class="main">
      <TopBar title={pageTitle} path={path} />
      {@render children()}
    </div>
  </div>
{:else}
  <div class="min-h-screen grid-noise flex items-center justify-center">
    <span class="auth-spinner" aria-label="Loading"></span>
  </div>
{/if}

<div class="toasts" aria-live="polite">
  {#each $toasts as t (t.id)}
    <div class="toast toast-{t.kind}">{t.message}</div>
  {/each}
</div>

<style>
  .auth-spinner {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 2px solid var(--color-border);
    border-top-color: var(--color-accent);
    animation: auth-spin 0.7s linear infinite;
  }
  @keyframes auth-spin {
    to { transform: rotate(360deg); }
  }
  .toasts {
    position: fixed;
    bottom: 20px;
    right: 20px;
    left: auto;
    transform: none;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 8px;
    z-index: 100;
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    min-width: 240px;
    max-width: 360px;
    padding: 11px 14px;
    border-radius: var(--radius-lg);
    background: var(--color-surface-glass-strong);
    backdrop-filter: blur(18px) saturate(150%);
    -webkit-backdrop-filter: blur(18px) saturate(150%);
    border: 1px solid var(--color-border-glass);
    color: var(--color-fg);
    font-size: 13px;
    box-shadow: var(--shadow-card);
    animation: toast-in 200ms var(--ease-out-expo);
  }
  .toast::before {
    content: '';
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 99px;
    margin-right: 9px;
    background: var(--color-accent);
    vertical-align: 1px;
  }
  .toast-error::before   { background: var(--color-danger); }
  .toast-success::before { background: var(--color-success); }
  .toast-warn::before    { background: var(--color-warn); }
  .toast-error   { border-color: color-mix(in srgb, var(--color-danger) 45%, var(--color-border-glass)); }
  .toast-success { border-color: color-mix(in srgb, var(--color-success) 45%, var(--color-border-glass)); }
  .toast-warn    { border-color: color-mix(in srgb, var(--color-warn) 45%, var(--color-border-glass)); }
  .toast-info    { border-color: color-mix(in srgb, var(--color-accent) 45%, var(--color-border-glass)); }
  @keyframes toast-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
