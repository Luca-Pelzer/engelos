<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type EventItem = { number: number; name: string; description: string; starts_at: string; ends_at?: string };
  type ListResponse = { channel: string; events: EventItem[] };

  const CHANNEL_KEY = 'engelos.channel';

  let channel = $state('');
  let events = $state<EventItem[]>([]);
  let loading = $state(false);

  let showForm = $state(false);
  let fName = $state('');
  let fDesc = $state('');
  let fStart = $state('');
  let fEnd = $state('');

  const now = Date.now();
  const sorted = $derived([...events].sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime()));

  function status(e: EventItem): 'live' | 'next' | 'upcoming' {
    const s = new Date(e.starts_at).getTime();
    const en = e.ends_at ? new Date(e.ends_at).getTime() : null;
    if (s <= now && en && en >= now) return 'live';
    return s > now ? 'upcoming' : 'upcoming';
  }

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) { toast('Bitte zuerst einen Kanal-Login eingeben.', 'warn'); return; }
    loading = true;
    try {
      const res = await api.get<ListResponse>(`/api/v1/liveops?channel=${encodeURIComponent(ch)}`);
      events = res.events ?? [];
      try { localStorage.setItem(CHANNEL_KEY, ch); } catch { /* ignore */ }
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'Event-Plan ist nicht aktiviert.' : 'Laden fehlgeschlagen.', 'error');
    } finally {
      loading = false;
    }
  }

  function openNew() { fName = ''; fDesc = ''; fStart = ''; fEnd = ''; showForm = true; }

  async function save() {
    const ch = channel.trim().toLowerCase();
    if (!ch || !fName.trim() || !fStart) { toast('Kanal, Name und Startzeit sind erforderlich.', 'warn'); return; }
    const body: Record<string, string> = { channel: ch, name: fName.trim(), description: fDesc, starts_at: new Date(fStart).toISOString() };
    if (fEnd) body.ends_at = new Date(fEnd).toISOString();
    try {
      const e = await api.post<EventItem>('/api/v1/liveops', body);
      events = [...events, e];
      showForm = false;
      toast('Event angelegt.', 'success');
    } catch {
      toast('Speichern fehlgeschlagen.', 'error');
    }
  }

  async function del(number: number) {
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(`/api/v1/liveops/${number}?channel=${encodeURIComponent(ch)}`);
      events = events.filter((x) => x.number !== number);
      toast('Event geloescht.', 'warn');
    } catch {
      toast('Loeschen fehlgeschlagen.', 'error');
    }
  }

  function fmt(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' });
  }

  onMount(() => {
    try {
      const saved = localStorage.getItem(CHANNEL_KEY);
      if (saved) { channel = saved; void load(); }
    } catch { /* ignore */ }
  });
</script>

<section class="page" data-screen-label="liveops">
  <div class="page-wrap">
    <div class="toolbar">
      <div class="input" style="max-width:200px">
        <input type="text" placeholder="Kanal-Login" bind:value={channel} onkeydown={(e) => { if (e.key === 'Enter') load(); }} />
      </div>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading}>{loading ? 'Laedt...' : 'Laden'}</button>
      <div class="grow"></div>
      <span class="count-pill"><b>{events.length}</b> Events</span>
      <button class="btn btn-primary btn-sm" onclick={openNew}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Neues Event</button>
    </div>

    {#if showForm}
      <div class="form-card">
        <div><label class="fld">Name</label><div class="input"><input type="text" placeholder="z. B. Subathon" bind:value={fName} /></div></div>
        <div><label class="fld">Beschreibung (optional)</label><div class="input"><input type="text" placeholder="Kurzbeschreibung" bind:value={fDesc} /></div></div>
        <div class="form-grid">
          <div><label class="fld">Start</label><div class="input"><input type="datetime-local" bind:value={fStart} /></div></div>
          <div><label class="fld">Ende (optional)</label><div class="input"><input type="datetime-local" bind:value={fEnd} /></div></div>
        </div>
        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Abbrechen</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Speichern</button>
        </div>
      </div>
    {/if}

    <div class="ev-list">
      {#each sorted as e (e.number)}
        <div class="ev-card">
          <div class="ev-when">
            <div class="ev-date">{fmt(e.starts_at)}</div>
            {#if status(e) === 'live'}<span class="ev-badge live">Laeuft jetzt</span>{:else}<span class="ev-badge">Geplant</span>{/if}
          </div>
          <div class="ev-main">
            <div class="ev-name">{e.name}</div>
            {#if e.description}<div class="ev-desc">{e.description}</div>{/if}
            {#if e.ends_at}<div class="ev-end">bis {fmt(e.ends_at)}</div>{/if}
          </div>
          <button class="iact del" onclick={() => del(e.number)} aria-label="Loeschen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
        </div>
      {/each}
    </div>

    {#if events.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5" width="17" height="16" rx="2.5" /><path d="M3.5 9.5h17M8 3v4M16 3v4" /></svg>
        <div class="t">{channel ? 'Keine Events geplant' : 'Kanal eingeben und laden'}</div>
        <div class="d">Plane deinen naechsten Stream oder Subathon.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .form-card { display: flex; flex-direction: column; gap: 14px; margin: 8px 0 4px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .form-actions { display: flex; gap: 10px; justify-content: flex-end; }
  .ev-list { display: flex; flex-direction: column; gap: 12px; margin-top: 14px; }
  .ev-card { position: relative; display: flex; gap: 18px; padding: 18px 20px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); box-shadow: var(--panel-shadow); }
  .ev-when { flex: none; width: 200px; display: flex; flex-direction: column; gap: 7px; }
  .ev-date { font-family: var(--mono); font-size: .9rem; color: var(--text-dim); font-weight: 600; }
  .ev-badge { display: inline-flex; align-self: flex-start; font-size: .7rem; font-weight: 700; letter-spacing: .03em; padding: 3px 9px; border-radius: 99px; color: var(--text-faint); background: var(--panel-3); }
  .ev-badge.live { color: #ff8a98; background: rgba(255,77,94,.12); }
  .ev-main { flex: 1; min-width: 0; }
  .ev-name { font-size: 1.05rem; font-weight: 800; letter-spacing: -.02em; }
  .ev-desc { color: var(--text-dim); font-size: .88rem; margin-top: 4px; line-height: 1.5; }
  .ev-end { color: var(--text-faint); font-size: .8rem; margin-top: 6px; font-family: var(--mono); }
  @media (max-width: 640px) { .form-grid { grid-template-columns: 1fr; } .ev-card { flex-direction: column; gap: 10px; } .ev-when { width: auto; flex-direction: row; align-items: center; } }
</style>
