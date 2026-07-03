<script lang="ts">
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Entry = {
    id: string;
    category: string;
    title: string;
    content: string;
    enabled: boolean;
    created_at: string;
    updated_at: string;
  };
  type ListResponse = { channel: string; query: string; entries: Entry[] };

  const categories = ['rules', 'schedule', 'games', 'faq', 'lore', 'commands', 'discord', 'other'];
  const categoryLabel: Record<string, string> = {
    rules: 'Rules', schedule: 'Schedule', games: 'Games', faq: 'FAQ',
    lore: 'Lore & gags', commands: 'Commands', discord: 'Discord', other: 'Other',
  };

  let channel = $state('');
  let entries = $state<Entry[]>([]);
  let loading = $state(false);
  let query = $state('');
  let filterCategory = $state('');

  let showForm = $state(false);
  let editing = $state<string | null>(null);
  let fCategory = $state('faq');
  let fTitle = $state('');
  let fContent = $state('');
  let fEnabled = $state(true);

  async function load() {
    if (!channel) return;
    loading = true;
    try {
      const params = new URLSearchParams();
      if (query.trim()) params.set('query', query.trim());
      if (filterCategory) params.set('category', filterCategory);
      const qs = params.toString();
      const res = await channelApi(channel).get<ListResponse>(`/kb${qs ? `?${qs}` : ''}`);
      entries = res.entries ?? [];
    } catch (err) {
      toast(err instanceof ApiException && err.status === 404 ? 'The knowledge base is not enabled.' : 'Could not load.', 'error');
    } finally {
      loading = false;
    }
  }

  function openNew() {
    editing = null;
    fCategory = 'faq';
    fTitle = '';
    fContent = '';
    fEnabled = true;
    showForm = true;
  }

  function openEdit(e: Entry) {
    editing = e.id;
    fCategory = e.category;
    fTitle = e.title;
    fContent = e.content;
    fEnabled = e.enabled;
    showForm = true;
  }

  async function save() {
    if (!fTitle.trim() || !fContent.trim()) { toast('Title and content are required.', 'warn'); return; }
    const body = { channel, category: fCategory, title: fTitle.trim(), content: fContent.trim(), enabled: fEnabled };
    try {
      if (editing) {
        const u = await channelApi(channel).put<Entry>(`/kb/${encodeURIComponent(editing)}`, body);
        entries = entries.map((x) => (x.id === editing ? u : x));
        toast('Entry updated.', 'success');
      } else {
        const c = await channelApi(channel).post<Entry>('/kb', body);
        entries = [c, ...entries];
        toast('Entry created.', 'success');
      }
      showForm = false;
    } catch (err) {
      toast(err instanceof ApiException && err.status === 400 ? 'Invalid entry (check length and category).' : 'Could not save.', 'error');
    }
  }

  async function toggle(e: Entry) {
    try {
      const u = await channelApi(channel).put<Entry>(`/kb/${encodeURIComponent(e.id)}`, {
        channel, category: e.category, title: e.title, content: e.content, enabled: !e.enabled,
      });
      entries = entries.map((x) => (x.id === e.id ? u : x));
    } catch {
      toast('Could not update.', 'error');
    }
  }

  async function del(id: string) {
    try {
      await channelApi(channel).delete(`/kb/${encodeURIComponent(id)}?channel=${encodeURIComponent(channel)}`);
      entries = entries.filter((x) => x.id !== id);
      toast('Entry deleted.', 'warn');
    } catch {
      toast('Could not delete.', 'error');
    }
  }

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void load(), 300);
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="knowledge">
  <div class="page-wrap">
    <div class="toolbar">
      <span class="ws-label">{channel ? `@${channel}` : 'No workspace selected'}</span>
      <div class="input" style="max-width:260px">
        <input type="text" placeholder="Search the knowledge base…" bind:value={query} oninput={onSearchInput} />
      </div>
      <div class="input" style="max-width:160px">
        <select bind:value={filterCategory} onchange={load}>
          <option value="">All categories</option>
          {#each categories as c (c)}<option value={c}>{categoryLabel[c]}</option>{/each}
        </select>
      </div>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading || !channel}>{loading ? 'Loading…' : 'Reload'}</button>
      <div class="grow"></div>
      <span class="count-pill"><b>{entries.length}</b> entries</span>
      <button class="btn btn-primary btn-sm" onclick={openNew} disabled={!channel}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>New entry</button>
    </div>

    <p class="kb-hint">Everything here feeds the <b>kb:lookup</b> workflow node — the bot retrieves the best-matching entries and answers viewer questions with them. Try the <b>kb-answer</b> template for a ready-made <code>!ask</code> command.</p>

    {#if showForm}
      <div class="form-card">
        <div class="form-grid">
          <div>
            <label class="fld" for="kb-category">Category</label>
            <div class="input"><select id="kb-category" bind:value={fCategory}>
              {#each categories as c (c)}<option value={c}>{categoryLabel[c]}</option>{/each}
            </select></div>
          </div>
          <label class="enable-row"><input type="checkbox" bind:checked={fEnabled} /> Enabled</label>
        </div>
        <div>
          <label class="fld" for="kb-title">Title</label>
          <div class="input"><input id="kb-title" type="text" placeholder="e.g. Stream schedule" bind:value={fTitle} maxlength="200" /></div>
        </div>
        <div>
          <label class="fld" for="kb-content">Content <span class="muted">({fContent.length}/4096)</span></label>
          <textarea id="kb-content" class="kb-textarea" rows="6" placeholder="The facts the bot should know and use in answers…" bind:value={fContent} maxlength="4096"></textarea>
        </div>
        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Cancel</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Save</button>
        </div>
      </div>
    {/if}

    <div class="kb-list">
      {#each entries as e (e.id)}
        <div class="kb-row" class:off={!e.enabled}>
          <div class="kb-main">
            <span class="kb-cat">{categoryLabel[e.category] ?? e.category}</span>
            <b class="kb-title">{e.title}</b>
          </div>
          <p class="kb-content">{e.content}</p>
          <div class="kb-side">
            <button class="switch" class:on={e.enabled} onclick={() => toggle(e)} aria-label="Toggle"></button>
            <button class="iact" onclick={() => openEdit(e)} aria-label="Edit"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg></button>
            <button class="iact del" onclick={() => del(e.id)} aria-label="Delete"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
          </div>
        </div>
      {/each}
    </div>

    {#if entries.length === 0 && !loading}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>
        <div class="t">{channel ? (query ? 'No matches' : 'No entries yet') : 'Select a workspace above'}</div>
        <div class="d">Teach the bot your rules, schedule, FAQ and lore — it answers viewers with these facts.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .kb-hint { margin: 4px 0 12px; font-size: .84rem; color: var(--text-dim); }
  .form-card { display: flex; flex-direction: column; gap: 14px; margin: 8px 0 14px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .form-grid { display: grid; grid-template-columns: 1fr auto; gap: 14px; align-items: end; }
  .form-actions { display: flex; gap: 10px; justify-content: flex-end; }
  .enable-row { display: flex; align-items: center; gap: 9px; font-size: .9rem; color: var(--text-dim); font-weight: 600; }
  .kb-textarea { width: 100%; font: inherit; font-size: .88rem; line-height: 1.55; padding: 10px 12px; border-radius: 10px; background: var(--bg); border: 1px solid var(--panel-border); color: inherit; resize: vertical; }
  .kb-list { display: flex; flex-direction: column; gap: 8px; }
  .kb-row { display: grid; grid-template-columns: minmax(180px, 240px) 1fr auto; gap: 14px; align-items: start; padding: 13px 16px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); transition: border-color .18s var(--ease), opacity .18s var(--ease); }
  .kb-row:hover { border-color: var(--border-strong); }
  .kb-row.off { opacity: .55; }
  .kb-main { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .kb-cat { align-self: flex-start; font-size: .64rem; font-weight: 800; letter-spacing: .05em; text-transform: uppercase; color: var(--text-faint); background: var(--panel-2); border: 1px solid var(--panel-border); padding: 2px 7px; border-radius: 6px; }
  .kb-title { font-size: .95rem; overflow: hidden; text-overflow: ellipsis; }
  .kb-content { margin: 0; font-size: .84rem; color: var(--text-dim); line-height: 1.55; display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
  .kb-side { display: flex; align-items: center; gap: 8px; }
  .grow { flex: 1 1 auto; }
  .muted { color: var(--text-dim); font-weight: 400; }
  .ws-label { font-weight: 700; color: var(--text-dim); font-size: .9rem; }
  @media (max-width: 760px) { .kb-row { grid-template-columns: 1fr auto; } .kb-content { grid-column: 1 / -1; } }
</style>
