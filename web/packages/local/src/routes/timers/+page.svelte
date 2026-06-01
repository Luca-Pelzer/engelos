<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Timer = { name: string; message: string; interval_seconds: number; min_chat_lines: number; enabled: boolean };
  type ListResponse = { channel: string; timers: Timer[] };

  const CHANNEL_KEY = 'engelos.channel';

  let channel = $state('');
  let timers = $state<Timer[]>([]);
  let loading = $state(false);

  let showForm = $state(false);
  let editing = $state<string | null>(null);
  let fName = $state('');
  let fMessage = $state('');
  let fIntervalMin = $state(10);
  let fMinLines = $state(0);
  let fEnabled = $state(true);

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) { toast('Bitte zuerst einen Kanal-Login eingeben.', 'warn'); return; }
    loading = true;
    try {
      const res = await api.get<ListResponse>(`/api/v1/timers?channel=${encodeURIComponent(ch)}`);
      timers = res.timers ?? [];
      try { localStorage.setItem(CHANNEL_KEY, ch); } catch { /* ignore */ }
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'Timers-Feature ist nicht aktiviert.' : 'Laden fehlgeschlagen.', 'error');
    } finally {
      loading = false;
    }
  }

  function openNew() { editing = null; fName = ''; fMessage = ''; fIntervalMin = 10; fMinLines = 0; fEnabled = true; showForm = true; }
  function openEdit(t: Timer) { editing = t.name; fName = t.name; fMessage = t.message; fIntervalMin = Math.round(t.interval_seconds / 60); fMinLines = t.min_chat_lines; fEnabled = t.enabled; showForm = true; }

  async function save() {
    const ch = channel.trim().toLowerCase();
    if (!ch || !fName.trim() || !fMessage.trim()) { toast('Kanal, Name und Nachricht sind erforderlich.', 'warn'); return; }
    const body = { channel: ch, name: fName.trim(), message: fMessage, interval_seconds: Math.max(1, fIntervalMin) * 60, min_chat_lines: fMinLines, enabled: fEnabled };
    try {
      if (editing) {
        const t = await api.put<Timer>(`/api/v1/timers/${encodeURIComponent(editing)}`, body);
        timers = timers.map((x) => (x.name === editing ? t : x));
        toast('Timer aktualisiert.', 'success');
      } else {
        const t = await api.post<Timer>('/api/v1/timers', body);
        timers = [...timers, t];
        toast('Timer angelegt.', 'success');
      }
      showForm = false;
    } catch (err) {
      toast(err instanceof ApiException && err.status === 409 ? 'Name existiert bereits.' : 'Speichern fehlgeschlagen.', 'error');
    }
  }

  async function toggle(t: Timer) {
    const ch = channel.trim().toLowerCase();
    try {
      const u = await api.put<Timer>(`/api/v1/timers/${encodeURIComponent(t.name)}`, { channel: ch, message: t.message, interval_seconds: t.interval_seconds, min_chat_lines: t.min_chat_lines, enabled: !t.enabled });
      timers = timers.map((x) => (x.name === t.name ? u : x));
    } catch {
      toast('Aenderung fehlgeschlagen.', 'error');
    }
  }

  async function del(name: string) {
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(`/api/v1/timers/${encodeURIComponent(name)}?channel=${encodeURIComponent(ch)}`);
      timers = timers.filter((x) => x.name !== name);
      toast('Timer geloescht.', 'warn');
    } catch {
      toast('Loeschen fehlgeschlagen.', 'error');
    }
  }

  onMount(() => {
    try {
      const saved = localStorage.getItem(CHANNEL_KEY);
      if (saved) { channel = saved; void load(); }
    } catch { /* ignore */ }
  });
</script>

<section class="page" data-screen-label="timers">
  <div class="page-wrap">
    <div class="toolbar">
      <div class="input" style="max-width:200px">
        <input type="text" placeholder="Kanal-Login" bind:value={channel} onkeydown={(e) => { if (e.key === 'Enter') load(); }} />
      </div>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading}>{loading ? 'Laedt...' : 'Laden'}</button>
      <div class="grow"></div>
      <span class="count-pill"><b>{timers.length}</b> Timer</span>
      <button class="btn btn-primary btn-sm" onclick={openNew}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Neuer Timer</button>
    </div>

    {#if showForm}
      <div class="form-card">
        <div class="form-grid">
          <div><label class="fld">Name</label><div class="input"><input type="text" placeholder="z. B. discord" bind:value={fName} disabled={editing !== null} /></div></div>
          <div><label class="fld">Intervall (Minuten)</label><div class="input"><input type="number" min="1" bind:value={fIntervalMin} /></div></div>
        </div>
        <div><label class="fld">Nachricht</label><div class="input"><input type="text" placeholder="Tritt unserem Discord bei: ..." bind:value={fMessage} /></div></div>
        <div class="form-grid">
          <div><label class="fld">Min. Chat-Zeilen seit letztem Post</label><div class="input"><input type="number" min="0" bind:value={fMinLines} /></div></div>
          <label class="enable-row"><input type="checkbox" bind:checked={fEnabled} /> Aktiviert</label>
        </div>
        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Abbrechen</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Speichern</button>
        </div>
      </div>
    {/if}

    <table class="dtable" style="margin-top:14px">
      <thead><tr><th>Name</th><th>Nachricht</th><th class="right" style="width:110px">Intervall</th><th style="width:90px">Aktiv</th><th class="right" style="width:96px">Aktion</th></tr></thead>
      <tbody>
        {#each timers as t (t.name)}
          <tr>
            <td><b>{t.name}</b></td>
            <td class="muted">{t.message}</td>
            <td class="right num">{Math.round(t.interval_seconds / 60)} min</td>
            <td><button class="switch" class:on={t.enabled} onclick={() => toggle(t)} aria-label="Umschalten"></button></td>
            <td>
              <div class="row-actions">
                <button class="iact" onclick={() => openEdit(t)} aria-label="Bearbeiten"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg></button>
                <button class="iact del" onclick={() => del(t.name)} aria-label="Loeschen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if timers.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="13" r="8" /><path d="M12 9v4l2.5 2M9 2.5h6" /></svg>
        <div class="t">{channel ? 'Keine Timer' : 'Kanal eingeben und laden'}</div>
        <div class="d">Lege wiederkehrende Ansagen an.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .form-card { display: flex; flex-direction: column; gap: 14px; margin: 8px 0 4px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; align-items: end; }
  .form-actions { display: flex; gap: 10px; justify-content: flex-end; }
  .enable-row { display: flex; align-items: center; gap: 9px; font-size: .9rem; color: var(--text-dim); font-weight: 600; }
  @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }
</style>
