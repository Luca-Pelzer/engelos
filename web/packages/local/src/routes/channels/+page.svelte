<script lang="ts">
  import { api, toast } from '@engelos/shared/lib';
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';

  type Workspace = { slug: string; display_name: string; twitch_login: string; role: string };

  const ACTIVE_KEY = 'engelos.workspace';

  let workspaces = $state<Workspace[]>([]);
  let loading = $state(true);

  async function load() {
    loading = true;
    try {
      const res = await api.get<{ workspaces: Workspace[] }>('/api/v1/me/workspaces');
      workspaces = res.workspaces ?? [];
    } catch {
      toast('Could not load workspaces.', 'error');
    } finally {
      loading = false;
    }
  }

  function open(slug: string) {
    try { localStorage.setItem(ACTIVE_KEY, slug); } catch { /* ignore */ }
    void goto(`/channels/${slug}`);
  }

  onMount(load);
</script>

<section class="page" data-screen-label="channels">
  <div class="page-wrap">
    <div class="head">
      <h2>Deine Workspaces</h2>
      <p class="muted">Pick a channel to manage or create a new one.</p>
    </div>

    {#if loading}
      <div class="muted">Loading…</div>
    {:else}
      <div class="grid">
        {#each workspaces as w (w.slug)}
          <button class="card" onclick={() => open(w.slug)}>
            <span class="mark">{w.display_name.charAt(0).toUpperCase()}</span>
            <span class="info">
              <span class="name">{w.display_name}</span>
              <span class="login">@{w.twitch_login}</span>
            </span>
            <span class="badge" class:owner={w.role === 'owner'}>{w.role}</span>
          </button>
        {/each}

        <a class="card new" href="/onboard">
          <span class="mark plus">+</span>
          <span class="info"><span class="name">New workspace</span><span class="login">Create your own channel</span></span>
        </a>
      </div>

      {#if workspaces.length === 0}
        <div class="empty">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="12" r="2.5"/><path d="M7.5 6H12a4 4 0 0 1 4 4v.5M7.5 18H12a4 4 0 0 0 4-4v-.5"/></svg>
          <div class="t">Noch kein Workspace</div>
          <div class="d">Lege Deinen eigenen Channel an, um zu starten.</div>
        </div>
      {/if}
    {/if}
  </div>
</section>

<style>
  .head { margin-bottom: 18px; }
  .head h2 { margin: 0 0 4px; font-size: 1.3rem; }
  .muted { color: var(--text-dim); }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 14px; }
  .card {
    display: flex; align-items: center; gap: 12px; padding: 16px; text-align: left;
    border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border);
    color: var(--text); cursor: pointer; font: inherit; text-decoration: none;
  }
  .card:hover { border-color: var(--brand); }
  .mark {
    width: 42px; height: 42px; flex: 0 0 auto; display: grid; place-items: center; border-radius: 10px;
    font-weight: 800; font-size: 18px; color: #fff;
    background: linear-gradient(135deg, var(--brand), var(--brand-2));
  }
  .mark.plus { background: var(--bg); color: var(--text-dim); border: 1px dashed var(--panel-border); }
  .info { display: flex; flex-direction: column; min-width: 0; }
  .name { font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .login { font-size: .82rem; color: var(--text-dim); }
  .badge {
    margin-left: auto; font-size: 10px; font-weight: 800; text-transform: uppercase; letter-spacing: .04em;
    padding: 3px 8px; border-radius: 999px; color: var(--text-dim); background: var(--bg); border: 1px solid var(--panel-border);
  }
  .badge.owner { color: #fff; background: linear-gradient(135deg, var(--brand), var(--brand-2)); border: 0; }
</style>
