<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { Sidebar, TopBar } from '@engelos/shared/components';
  import { ws, toasts } from '@engelos/shared/lib';

  let { children } = $props();

  const path = $derived($page.url.pathname);

  const isChromeless = $derived(
    path === '/login' || path === '/setup' || path.startsWith('/setup/'),
  );

  const pageTitle = $derived.by(() => {
    if (path === '/')              return 'Dashboard';
    if (path.startsWith('/chat'))         return 'Chat';
    if (path.startsWith('/commands'))     return 'Commands';
    if (path.startsWith('/redemptions'))  return 'Channel Points';
    if (path.startsWith('/counters'))     return 'Counters';
    if (path.startsWith('/automod'))      return 'AutoMod';
    if (path.startsWith('/translate'))    return 'Translation';
    if (path.startsWith('/clipper'))      return 'Auto-Clipper';
    if (path.startsWith('/pity'))         return 'Pity';
    if (path.startsWith('/streak'))       return 'Streak';
    if (path.startsWith('/wrapped'))      return 'Stream Wrapped';
    if (path.startsWith('/connections'))  return 'Connections';
    if (path.startsWith('/integrations')) return 'Integrations';
    if (path.startsWith('/settings'))     return 'Settings';
    if (path.startsWith('/upgrade'))      return 'Upgrade to Cloud';
    return '';
  });

  onMount(() => {
    ws.connect();
    return () => ws.disconnect();
  });
</script>

{#if isChromeless}
  <main class="min-h-screen grid-noise">
    {@render children()}
  </main>
{:else}
  <div class="flex min-h-screen bg-[var(--color-bg)]">
    <Sidebar current={path} />
    <div class="flex-1 flex flex-col min-w-0">
      <TopBar title={pageTitle} />
      <main class="flex-1 px-7 py-7 max-w-[1280px] w-full mx-auto">
        {@render children()}
      </main>
    </div>
  </div>
{/if}

<div class="toasts" aria-live="polite">
  {#each $toasts as t (t.id)}
    <div class="toast toast-{t.kind}">{t.message}</div>
  {/each}
</div>

<style>
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
