<script lang="ts">
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Quote = { number: number; text: string; created_by: string; created_at: string };
  type ListResponse = { channel: string; quotes: Quote[] };

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
    if (!channel) { return; }
    loading = true;
    try {
      const res = await channelApi(channel).get<ListResponse>('/quotes');
      quotes = res.quotes ?? [];
    } catch (err) {
      const msg = err instanceof ApiException && err.status === 501 ? 'The quotes feature is not enabled.' : 'Could not load.';
      toast(msg, 'error');
    } finally {
      loading = false;
    }
  }

  async function add() {
    const text = draft.trim();
    if (!channel || !text) {
      toast('Quote text is required.', 'warn');
      return;
    }
    try {
      const q = await channelApi(channel).post<Quote>('/quotes', { text });
      quotes = [...quotes, q];
      draft = '';
      showAdd = false;
      toast(`Quote #${q.number} saved.`, 'success');
    } catch {
      toast('Could not save.', 'error');
    }
  }

  async function del(n: number) {
    try {
      await channelApi(channel).delete(`/quotes/${n}`);
      quotes = quotes.filter((x) => x.number !== n);
      toast(`Quote #${n} deleted.`, 'warn');
    } catch {
      toast('Could not delete.', 'error');
    }
  }

  function fmtDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('de-DE');
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="quotes">
  <div class="page-wrap">
    <div class="section-title">Quote Plugin <span class="sub">optional content store for chat and workflow actions</span></div>
    <div class="toolbar">
      <span class="ws-label">{channel ? `@${channel}` : 'No workspace selected'}</span>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading || !channel}>{loading ? 'Loading…' : 'Reload'}</button>
      <div class="input search">
        <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3-3" /></svg></span>
        <input type="text" placeholder="Durchsuchen... (Text oder #Nummer)" bind:value={search} />
      </div>
      <div class="grow"></div>
      <span class="count-pill"><b>{quotes.length}</b> quotes</span>
      <button class="btn btn-primary btn-sm" onclick={() => (showAdd = true)}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Add quote</button>
    </div>

    {#if showAdd}
      <div class="add-row">
        <textarea class="add-text" placeholder='"..."' bind:value={draft}></textarea>
        <div class="add-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => { showAdd = false; draft = ''; }}>Cancel</button>
          <button class="btn btn-primary btn-sm" onclick={add}>Save</button>
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
          <button class="iact del q-del" onclick={() => del(x.number)} aria-label="Delete">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13" /></svg>
          </button>
        </div>
      {/each}
    </div>

    {#if filtered.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7 7h4v4a4 4 0 0 1-4 4M13 7h4v4a4 4 0 0 1-4 4" /></svg>
        <div class="t">{channel ? 'No quotes found' : 'Select a workspace above'}</div>
        <div class="d">Save the next legendary moment.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .ws-label { font-weight: 700; color: var(--text-dim); font-size: .9rem; }
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
