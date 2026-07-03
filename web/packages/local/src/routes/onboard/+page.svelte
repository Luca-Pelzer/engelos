<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { goto } from '$app/navigation';

  const ACTIVE_KEY = 'engelos.workspace';

  let slug = $state('');
  let displayName = $state('');
  let saving = $state(false);

  type Workspace = { slug: string; display_name: string; twitch_login: string; role: string };

  async function create() {
    const s = slug.trim().toLowerCase();
    if (!s) { toast('Enter the channel login first.', 'warn'); return; }
    saving = true;
    try {
      const ws = await api.post<Workspace>('/api/v1/workspaces', { slug: s, display_name: displayName.trim() });
      try { localStorage.setItem(ACTIVE_KEY, ws.slug); } catch { /* ignore */ }
      toast('Workspace angelegt.', 'success');
      void goto(`/channels/${ws.slug}`);
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) toast('Dieser Workspace existiert bereits.', 'error');
      else toast('Could not create.', 'error');
    } finally {
      saving = false;
    }
  }
</script>

<section class="page" data-screen-label="onboard">
  <div class="wrap">
    <div class="form-card">
      <div class="brand"><span class="mark">E</span></div>
      <h2>Create workspace</h2>
      <p class="muted">Connect your channel to set up your bot.</p>

      <div><label class="fld" for="onboard-slug">Channel login</label><div class="input"><input id="onboard-slug" type="text" placeholder="e.g. yourtwitchname" bind:value={slug} onkeydown={(e) => { if (e.key === 'Enter') create(); }} /></div></div>
      <div><label class="fld" for="onboard-display">Display name (optional)</label><div class="input"><input id="onboard-display" type="text" placeholder="Derived from your login if empty" bind:value={displayName} /></div></div>

      <div class="actions">
        <a class="btn btn-ghost btn-sm" href="/channels">Cancel</a>
        <button class="btn btn-primary btn-sm" onclick={create} disabled={saving}>{saving ? 'Creating…' : 'Create workspace'}</button>
      </div>
    </div>
  </div>
</section>

<style>
  .wrap { display: grid; place-items: center; min-height: 70vh; }
  .form-card { display: flex; flex-direction: column; gap: 14px; width: 100%; max-width: 420px; padding: 26px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .brand { display: flex; justify-content: center; }
  .mark { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 12px; font-weight: 800; font-size: 22px; color: #fff; background: linear-gradient(135deg, var(--brand), var(--brand-2)); }
  .form-card h2 { margin: 6px 0 0; text-align: center; font-size: 1.3rem; }
  .muted { margin: 0 0 6px; text-align: center; color: var(--text-dim); font-size: .9rem; }
  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 4px; }
</style>
