<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Reward = { name: string; cost: number; description: string };
  type ListResponse = { channel: string; rewards: Reward[] };

  const CHANNEL_KEY = 'engelos.channel';

  let channel = $state('');
  let rewards = $state<Reward[]>([]);
  let search = $state('');
  let loading = $state(false);

  let showForm = $state(false);
  let editing = $state<string | null>(null);
  let fName = $state('');
  let fCost = $state(0);
  let fDesc = $state('');

  const filtered = $derived(
    rewards.filter((r) => !search.trim() || r.name.toLowerCase().includes(search.trim().toLowerCase())),
  );

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) { toast('Bitte zuerst einen Kanal-Login eingeben.', 'warn'); return; }
    loading = true;
    try {
      const res = await api.get<ListResponse>(`/api/v1/rewards?channel=${encodeURIComponent(ch)}`);
      rewards = res.rewards ?? [];
      try { localStorage.setItem(CHANNEL_KEY, ch); } catch { /* ignore */ }
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'Rewards-Feature ist nicht aktiviert.' : 'Laden fehlgeschlagen.', 'error');
    } finally {
      loading = false;
    }
  }

  function openNew() { editing = null; fName = ''; fCost = 0; fDesc = ''; showForm = true; }
  function openEdit(r: Reward) { editing = r.name; fName = r.name; fCost = r.cost; fDesc = r.description; showForm = true; }

  async function save() {
    const ch = channel.trim().toLowerCase();
    if (!ch || !fName.trim()) { toast('Kanal und Name sind erforderlich.', 'warn'); return; }
    try {
      if (editing) {
        const r = await api.put<Reward>(`/api/v1/rewards/${encodeURIComponent(editing)}`, { channel: ch, cost: fCost, description: fDesc });
        rewards = rewards.map((x) => (x.name === editing ? r : x));
        toast('Belohnung aktualisiert.', 'success');
      } else {
        const r = await api.post<Reward>('/api/v1/rewards', { channel: ch, name: fName.trim(), cost: fCost, description: fDesc });
        rewards = [...rewards, r];
        toast('Belohnung angelegt.', 'success');
      }
      showForm = false;
    } catch (err) {
      toast(err instanceof ApiException && err.status === 409 ? 'Name existiert bereits.' : 'Speichern fehlgeschlagen.', 'error');
    }
  }

  async function del(name: string) {
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(`/api/v1/rewards/${encodeURIComponent(name)}?channel=${encodeURIComponent(ch)}`);
      rewards = rewards.filter((x) => x.name !== name);
      toast('Belohnung geloescht.', 'warn');
    } catch {
      toast('Loeschen fehlgeschlagen.', 'error');
    }
  }

  function fmt(n: number): string { return n.toLocaleString('de-DE'); }

  onMount(() => {
    try {
      const saved = localStorage.getItem(CHANNEL_KEY);
      if (saved) { channel = saved; void load(); }
    } catch { /* ignore */ }
  });
</script>

<section class="page" data-screen-label="rewards">
  <div class="page-wrap">
    <div class="toolbar">
      <div class="input" style="max-width:200px">
        <input type="text" placeholder="Kanal-Login" bind:value={channel} onkeydown={(e) => { if (e.key === 'Enter') load(); }} />
      </div>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading}>{loading ? 'Laedt...' : 'Laden'}</button>
      <div class="input search">
        <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3-3" /></svg></span>
        <input type="text" placeholder="Belohnungen durchsuchen..." bind:value={search} />
      </div>
      <div class="grow"></div>
      <span class="count-pill"><b>{rewards.length}</b> Belohnungen</span>
      <button class="btn btn-primary btn-sm" onclick={openNew}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Neue Belohnung</button>
    </div>

    {#if showForm}
      <div class="form-card">
        <div class="form-grid">
          <div><label class="fld">Name</label><div class="input"><input type="text" placeholder="z. B. Songwunsch" bind:value={fName} disabled={editing !== null} /></div></div>
          <div><label class="fld">Punkte-Kosten</label><div class="input"><input type="number" min="0" bind:value={fCost} /></div></div>
        </div>
        <div><label class="fld">Beschreibung (optional)</label><div class="input"><input type="text" placeholder="Kurzbeschreibung" bind:value={fDesc} /></div></div>
        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Abbrechen</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Speichern</button>
        </div>
      </div>
    {/if}

    <table class="dtable" style="margin-top:14px">
      <thead><tr><th>Belohnung</th><th>Beschreibung</th><th class="right">Kosten</th><th class="right" style="width:96px">Aktion</th></tr></thead>
      <tbody>
        {#each filtered as r (r.name)}
          <tr>
            <td><b>{r.name}</b></td>
            <td class="muted">{r.description}</td>
            <td class="right num">{fmt(r.cost)}</td>
            <td>
              <div class="row-actions">
                <button class="iact" onclick={() => openEdit(r)} aria-label="Bearbeiten"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg></button>
                <button class="iact del" onclick={() => del(r.name)} aria-label="Loeschen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if filtered.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="8.5" width="17" height="12" rx="1.6" /><path d="M3.5 12.5h17M12 8.5v12" /></svg>
        <div class="t">{channel ? 'Keine Belohnungen' : 'Kanal eingeben und laden'}</div>
        <div class="d">Lege deine erste Belohnung an.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .form-card { display: flex; flex-direction: column; gap: 14px; margin: 8px 0 4px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .form-actions { display: flex; gap: 10px; justify-content: flex-end; }
  @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }
</style>
