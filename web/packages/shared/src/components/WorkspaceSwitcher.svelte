<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib';

  type Workspace = { slug: string; display_name: string; twitch_login: string; role: string };

  const ACTIVE_KEY = 'engelos.workspace';

  let workspaces = $state<Workspace[]>([]);
  let open = $state(false);
  let activeSlug = $state('');
  let loaded = $state(false);

  const active = $derived(workspaces.find((w) => w.slug === activeSlug) ?? workspaces[0]);

  async function load() {
    try {
      const res = await api.get<{ workspaces: Workspace[] }>('/api/v1/me/workspaces');
      workspaces = res.workspaces ?? [];
      try {
        const saved = localStorage.getItem(ACTIVE_KEY);
        if (saved && workspaces.some((w) => w.slug === saved)) {
          activeSlug = saved;
        } else {
          const owner = workspaces.find((w) => w.role === 'owner');
          activeSlug = owner?.slug ?? workspaces[0]?.slug ?? '';
        }
      } catch {
        activeSlug = workspaces[0]?.slug ?? '';
      }
    } catch {
      workspaces = [];
    } finally {
      loaded = true;
    }
  }

  function select(slug: string) {
    activeSlug = slug;
    try { localStorage.setItem(ACTIVE_KEY, slug); } catch { /* ignore */ }
    open = false;
    window.location.assign(`/channels/${slug}`);
  }

  function newWorkspace() {
    open = false;
    window.location.assign('/onboard');
  }

  onMount(load);
</script>

{#if loaded}
  {#if workspaces.length === 0}
    <button class="ws-empty" onclick={newWorkspace}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>
      Create workspace
    </button>
  {:else}
    <div class="ws" class:open>
      <button class="ws-current" onclick={() => (open = !open)} aria-label="Workspace wechseln">
        <span class="ws-mark" aria-hidden="true">{(active?.display_name ?? '?').charAt(0).toUpperCase()}</span>
        <span class="ws-text">
          <span class="ws-name">{active?.display_name ?? 'Workspace'}</span>
          <span class="ws-role">{active?.role}</span>
        </span>
        <svg class="ws-caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6" /></svg>
      </button>

      {#if open}
        <button class="ws-scrim" onclick={() => (open = false)} aria-label="Schliessen"></button>
        <div class="ws-menu">
          {#each workspaces as w (w.slug)}
            <button class="ws-item" class:active={w.slug === activeSlug} onclick={() => select(w.slug)}>
              <span class="ws-mark sm" aria-hidden="true">{w.display_name.charAt(0).toUpperCase()}</span>
              <span class="ws-text">
                <span class="ws-name">{w.display_name}</span>
                <span class="ws-login">@{w.twitch_login}</span>
              </span>
              <span class="ws-badge" class:owner={w.role === 'owner'}>{w.role}</span>
            </button>
          {/each}
          <div class="ws-sep"></div>
          <button class="ws-item new" onclick={newWorkspace}>
            <span class="ws-mark sm plus" aria-hidden="true">+</span>
            <span class="ws-name">Neuer Workspace</span>
          </button>
        </div>
      {/if}
    </div>
  {/if}
{/if}

<style>
  .ws { position: relative; }
  .ws-current, .ws-empty {
    display: inline-flex; align-items: center; gap: 9px; height: 38px; padding: 0 10px;
    border-radius: var(--radius, 10px); background: var(--panel-2, #161b26);
    border: 1px solid var(--border, #252c3b); color: var(--text, #f4f6fb);
    cursor: pointer; font: inherit;
  }
  .ws-current:hover, .ws-empty:hover { border-color: var(--brand, #ff5d73); }
  .ws-empty { gap: 7px; font-weight: 600; font-size: 13px; color: var(--text-dim, #aeb6c6); }
  .ws-empty svg { width: 16px; height: 16px; }
  .ws-mark {
    width: 26px; height: 26px; flex: 0 0 auto; display: grid; place-items: center;
    border-radius: 7px; font-weight: 800; font-size: 13px; color: #fff;
    background: linear-gradient(135deg, var(--brand, #ff5d73), var(--brand-2, #ff9e3d));
  }
  .ws-mark.sm { width: 24px; height: 24px; font-size: 12px; }
  .ws-mark.plus { background: var(--panel-2, #161b26); color: var(--text-dim, #aeb6c6); border: 1px dashed var(--border, #252c3b); }
  .ws-text { display: flex; flex-direction: column; align-items: flex-start; line-height: 1.15; min-width: 0; }
  .ws-name { font-size: 13px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 140px; }
  .ws-role, .ws-login { font-size: 11px; color: var(--text-dim, #aeb6c6); }
  .ws-caret { width: 16px; height: 16px; color: var(--text-dim, #aeb6c6); }
  .ws-scrim { position: fixed; inset: 0; z-index: 40; background: transparent; border: 0; cursor: default; }
  .ws-menu {
    position: absolute; top: calc(100% + 6px); left: 0; z-index: 41; min-width: 240px;
    display: flex; flex-direction: column; gap: 2px; padding: 6px;
    border-radius: var(--radius, 10px); background: var(--panel, #10131c);
    border: 1px solid var(--border, #252c3b); box-shadow: 0 18px 40px rgba(0, 0, 0, .45);
  }
  .ws-item {
    display: flex; align-items: center; gap: 9px; padding: 7px 8px; border-radius: 8px;
    background: transparent; border: 0; color: var(--text, #f4f6fb); cursor: pointer; font: inherit; text-align: left;
  }
  .ws-item:hover { background: var(--panel-2, #161b26); }
  .ws-item.active { background: var(--panel-2, #161b26); }
  .ws-badge {
    margin-left: auto; font-size: 10px; font-weight: 800; text-transform: uppercase; letter-spacing: .04em;
    padding: 2px 7px; border-radius: 999px; color: var(--text-dim, #aeb6c6); background: var(--panel-2, #161b26);
    border: 1px solid var(--border, #252c3b);
  }
  .ws-badge.owner { color: #fff; background: linear-gradient(135deg, var(--brand, #ff5d73), var(--brand-2, #ff9e3d)); border: 0; }
  .ws-sep { height: 1px; margin: 4px 2px; background: var(--border, #252c3b); }
  .ws-item.new .ws-name { color: var(--text-dim, #aeb6c6); font-weight: 600; }
</style>
