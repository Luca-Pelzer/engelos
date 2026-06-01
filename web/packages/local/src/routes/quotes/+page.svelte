<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Quote = { number: number; text: string; created_by: string; created_at: string };
  type ListResponse = { channel: string; quotes: Quote[] };

  const CHANNEL_KEY = 'engelos.channel';

  let channel = $state('');
  let quotes = $state<Quote[]>([]);
  let search = $state('');
  let loading = $state(false);
  let showAdd = $state(false);
  let draft = $state('');

  const filtered = $derived.by(() => {
    const raw = search.trim();
    if (!raw) return [...quotes].sort((a, b) => b.number - a.number);
    const byNum = /^#?\d+$/.test(raw);
    const q = raw.replace(/^#/, '').toLowerCase();
    return quotes
      .filter((x) => (byNum ? String(x.number) === raw.replace(/^#/, '') : x.text.toLowerCase().includes(q)))
      .sort((a, b) => b.number - a.number);
  });

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Bitte zuerst einen Kanal-Login eingeben.', 'warn');
      return;
    }
    loading = true;
    try {
      const res = await api.get<ListResponse>(`/api/v1/quotes?channel=${encodeURIComponent(ch)}`);
      quotes = res.quotes ?? [];
      try { localStorage.setItem(CHANNEL_KEY, ch); } catch { /* ignore */ }
    } catch (err) {
      const msg = err instanceof ApiException && err.status === 501 ? 'Quotes-Feature ist nicht aktiviert.' : 'Laden fehlgeschlagen.';
      toast(msg, 'error');
    } finally {
      loading = false;
    }
  }

  async function add() {
    const ch = channel.trim().toLowerCase();
    const text = draft.trim();
    if (!ch || !text) {
      toast('Kanal und Zitat-Text sind erforderlich.', 'warn');
      return;
    }
    try {
      const q = await api.post<Quote>('/api/v1/quotes', { channel: ch, text });
      quotes = [...quotes, q];
      draft = '';
      showAdd = false;
      toast(`Zitat #${q.number} gespeichert.`, 'success');
    } catch {
      toast('Speichern fehlgeschlagen.', 'error');
    }
  }

  async function del(n: number) {
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(`/api/v1/quotes/${n}?channel=${encodeURIComponent(ch)}`);
      quotes = quotes.filter((x) => x.number !== n);
      toast(`Zitat #${n} geloescht.`, 'warn');
    } catch {
      toast('Loeschen fehlgeschlagen.', 'error');
    }
  }

  function fmtDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('de-DE');
  }

  onMount(() => {
    try {
      const saved = localStorage.getItem(CHANNEL_KEY);
      if (saved) { channel = saved; void load(); }
    } catch { /* ignore */ }
  });
</script>

<section class="page" data-screen-label="quotes">
  <div class="page-wrap">
    <div class="toolbar">
      <div class="input" style="max-width:220px">
        <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5h7M9 3v2c0 4-2 7-5 8" /></svg></span>
        <input type="text" placeholder="Kanal-Login" bind:value={channel} onkeydown={(e) => { if (e.key === 'Enter') load(); }} />
      </div>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading}>{loading ? 'Laedt...' : 'Laden'}</button>
      <div class="input search">
        <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3-3" /></svg></span>
        <input type="text" placeholder="Durchsuchen... (Text oder #Nummer)" bind:value={search} />
      </div>
      <div class="grow"></div>
      <span class="count-pill"><b>{quotes.length}</b> Zitate</span>
      <button class="btn btn-primary btn-sm" onclick={() => (showAdd = true)}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Zitat hinzufuegen</button>
    </div>

    {#if showAdd}
      <div class="add-row">
        <textarea class="add-text" placeholder='"..."' bind:value={draft}></textarea>
        <div class="add-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => { showAdd = false; draft = ''; }}>Abbrechen</button>
          <button class="btn btn-primary btn-sm" onclick={add}>Speichern</button>
        </div>
      </div>
    {/if}

    <div class="q-list">
      {#each filtered as x (x.number)}
        <div class="q-card">
          <div class="q-num">#{x.number}</div>
          <div class="q-main">
            <div class="q-text">{x.text}</div>
            <div class="q-meta">
              {#if x.created_by}<span class="who">{x.created_by}</span><span class="dot"></span>{/if}
              <span>{fmtDate(x.created_at)}</span>
            </div>
          </div>
          <button class="iact del q-del" onclick={() => del(x.number)} aria-label="Loeschen">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13" /></svg>
          </button>
        </div>
      {/each}
    </div>

    {#if filtered.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7 7h4v4a4 4 0 0 1-4 4M13 7h4v4a4 4 0 0 1-4 4" /></svg>
        <div class="t">{channel ? 'Keine Zitate gefunden' : 'Kanal eingeben und laden'}</div>
        <div class="d">Speichere den naechsten legendaeren Moment.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .add-row { display: flex; flex-direction: column; gap: 10px; margin: 8px 0 4px; padding: 16px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .add-text { width: 100%; border: 1px solid var(--border); background: var(--field); border-radius: var(--radius-sm); padding: 10px 12px; color: var(--text); font: inherit; resize: vertical; min-height: 70px; outline: none; }
  .add-text:focus { border-color: var(--brand); box-shadow: 0 0 0 4px var(--brand-glow); }
  .add-actions { display: flex; gap: 10px; justify-content: flex-end; }
  .q-list { display: flex; flex-direction: column; gap: 12px; margin-top: 14px; }
  .q-card { position: relative; display: flex; gap: 16px; padding: 18px 20px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); box-shadow: var(--panel-shadow); transition: .16s; }
  .q-card:hover { border-color: var(--border-strong); }
  .q-num { flex: none; font-family: var(--mono); font-weight: 700; font-size: 1.05rem; color: var(--brand); min-width: 48px; height: 40px; display: grid; place-items: center; border-radius: 11px; background: color-mix(in srgb, var(--brand) 13%, transparent); border: 1px solid color-mix(in srgb, var(--brand) 28%, transparent); }
  .q-main { flex: 1; min-width: 0; }
  .q-text { font-size: 1.02rem; line-height: 1.55; color: var(--text); }
  .q-text::before { content: '"'; color: var(--text-faint); font-weight: 700; }
  .q-text::after { content: '"'; color: var(--text-faint); font-weight: 700; }
  .q-meta { display: flex; align-items: center; gap: 12px; margin-top: 9px; color: var(--text-faint); font-size: .82rem; flex-wrap: wrap; }
  .q-meta .who { color: var(--text-dim); font-weight: 600; }
  .q-meta .dot { width: 3px; height: 3px; border-radius: 50%; background: var(--text-faint); }
  .q-del { position: absolute; top: 14px; right: 14px; opacity: 0; transition: .16s; }
  .q-card:hover .q-del { opacity: 1; }
  @media (max-width: 760px) { .q-del { opacity: 1; } }
</style>
