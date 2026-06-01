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
    if (path === '/')              return 'Dashboard';
    if (path.startsWith('/chat'))         return 'Chat';
    if (path.startsWith('/commands'))     return 'Commands';
    if (path.startsWith('/redemptions'))  return 'Channel Points';
    if (path.startsWith('/counters'))     return 'Counters';
    if (path.startsWith('/automod'))      return 'AutoMod';
    if (path.startsWith('/translate'))    return 'Translation';
    if (path.startsWith('/cohost'))       return 'AI Co-Host';
    if (path.startsWith('/contextmod'))   return 'AI Moderation';
    if (path.startsWith('/clipper'))      return 'Auto-Clipper';
    if (path.startsWith('/songrequests')) return 'Song Requests';
    if (path.startsWith('/pity'))         return 'Pity';
    if (path.startsWith('/streak'))       return 'Streak';
    if (path.startsWith('/wrapped'))      return 'Stream Wrapped';
    if (path.startsWith('/connections'))  return 'Connections';
    if (path.startsWith('/integrations')) return 'Integrationen';
    if (path.startsWith('/settings'))     return 'Einstellungen';
    if (path.startsWith('/upgrade'))      return 'Upgrade to Cloud';
    if (path.startsWith('/loyalty'))      return 'Punkte & Games';
    if (path.startsWith('/rewards'))      return 'Belohnungen';
    if (path.startsWith('/timers'))       return 'Auto-Ansagen';
    if (path.startsWith('/quotes'))       return 'Zitate';
    if (path.startsWith('/liveops'))      return 'Event-Plan';
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
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 100;
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    min-width: 240px;
    max-width: 360px;
    padding: 11px 14px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
    box-shadow: var(--shadow-lg);
    animation: toast-in 200ms var(--ease-out-expo);
  }
  .toast-error   { border-color: var(--color-danger); }
  .toast-success { border-color: var(--color-success); }
  .toast-warn    { border-color: var(--color-warn); }
  .toast-info    { border-color: var(--color-accent); }
  @keyframes toast-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
