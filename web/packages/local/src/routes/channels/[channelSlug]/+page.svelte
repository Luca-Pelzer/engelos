<script lang="ts">
  import { api, ApiException } from '@engelos/shared/lib';
  import { page } from '$app/stores';
  import { onMount } from 'svelte';

  type Workspace = { slug: string; display_name: string; twitch_login: string; auto_verify_mods: boolean; role: string };

  const ACTIVE_KEY = 'engelos.workspace';

  let ws = $state<Workspace | null>(null);
  let errorCode = $state('');
  let loading = $state(true);

  async function load(slug: string) {
    loading = true;
    errorCode = '';
    try { localStorage.setItem(ACTIVE_KEY, slug); } catch { /* ignore */ }
    try {
      ws = await api.get<Workspace>(`/api/v1/channels/${encodeURIComponent(slug)}`);
    } catch (err) {
      errorCode = err instanceof ApiException ? err.status === 404 ? 'not_found' : 'forbidden' : 'error';
      ws = null;
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    const slug = $page.params.channelSlug;
    if (slug) { void load(slug); } else { errorCode = 'not_found'; loading = false; }
  });
</script>

<section class="page" data-screen-label="channel">
  <div class="page-wrap">
    {#if loading}
      <div class="muted">Laedt...</div>
    {:else if ws}
      <div class="welcome">
        <span class="mark">{ws.display_name.charAt(0).toUpperCase()}</span>
        <div class="meta">
          <h2>{ws.display_name}</h2>
          <span class="login">@{ws.twitch_login}</span>
        </div>
        <span class="badge" class:owner={ws.role === 'owner'}>{ws.role}</span>
      </div>
      <div class="note">
        <b>Workspace aktiv.</b> Die Feature-Seiten (Commands, Aktionen, Timer, AutoMod ...) folgen hier in Kuerze unter diesem Channel.
      </div>
    {:else}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2l8 4v6c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V6z"/></svg>
        <div class="t">{errorCode === 'not_found' ? 'Channel nicht gefunden' : errorCode === 'forbidden' ? 'Kein Zugriff' : 'Fehler'}</div>
        <div class="d">{errorCode === 'forbidden' ? 'Du bist kein Mitglied dieses Workspace.' : 'Bitte einen Workspace aus der Liste waehlen.'}</div>
        <a class="btn btn-primary btn-sm" href="/channels" style="margin-top:12px">Zu meinen Workspaces</a>
      </div>
    {/if}
  </div>
</section>

<style>
  .muted { color: var(--text-dim); }
  .welcome { display: flex; align-items: center; gap: 14px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .mark { width: 48px; height: 48px; flex: 0 0 auto; display: grid; place-items: center; border-radius: 12px; font-weight: 800; font-size: 20px; color: #fff; background: linear-gradient(135deg, var(--brand), var(--brand-2)); }
  .meta { display: flex; flex-direction: column; }
  .meta h2 { margin: 0; font-size: 1.2rem; }
  .login { font-size: .85rem; color: var(--text-dim); }
  .badge { margin-left: auto; font-size: 10px; font-weight: 800; text-transform: uppercase; letter-spacing: .04em; padding: 3px 8px; border-radius: 999px; color: var(--text-dim); background: var(--bg); border: 1px solid var(--panel-border); }
  .badge.owner { color: #fff; background: linear-gradient(135deg, var(--brand), var(--brand-2)); border: 0; }
  .note { margin-top: 14px; padding: 14px 16px; border-radius: var(--radius); background: var(--bg); border: 1px solid var(--panel-border); color: var(--text-dim); font-size: .92rem; }
  .note b { color: var(--text); }
</style>
