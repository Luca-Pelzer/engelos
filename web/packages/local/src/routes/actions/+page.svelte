<script lang="ts">
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type PluginDef = { id: string; name: string; description: string };
  type Catalog = { conditions: PluginDef[]; actions: PluginDef[]; triggers: PluginDef[] };
  type CondInstance = { type_id: string; config: Record<string, unknown> };
  type ActInstance = { type_id: string; config: Record<string, unknown>; enabled: boolean };
  type ConditionList = { mode: string; conditions: CondInstance[] };
  type ActionList = { actions: ActInstance[]; queue_id?: string };
  type Rule = {
    name: string;
    enabled: boolean;
    trigger_kind: string;
    trigger_filter: Record<string, unknown> | null;
    conditions: ConditionList;
    actions: ActionList;
  };
  type ListResponse = { channel: string; rules: Rule[] };
  type RunHeader = {
    id: string;
    rule_name: string;
    trigger_kind: string;
    trigger_summary: string;
    started_at: string;
    finished_at: string;
    status: string;
    node_count: number;
  };
  type RunNode = {
    seq: number;
    node_kind: string;
    type_id: string;
    status: string;
    duration_ms: number;
    error?: string;
    output_summary?: string;
  };
  type RunDetail = RunHeader & { nodes?: RunNode[] };
  type RunsResponse = { channel: string; rule: string; runs: RunHeader[] | null };

  let channel = $state('');
  let rules = $state<Rule[]>([]);
  let catalog = $state<Catalog>({ conditions: [], actions: [], triggers: [] });
  let loading = $state(false);
  let firing = $state<string | null>(null);

  const triggerOptions: PluginDef[] = [
    { id: 'event', name: 'Event / Message', description: '' },
    { id: 'command', name: 'Command', description: '' },
    { id: 'timer', name: 'Timer / Interval', description: '' },
    { id: 'webhook', name: 'Inbound Webhook', description: '' },
    { id: 'manual', name: 'Manual', description: '' },
  ];

  let showForm = $state(false);
  let editing = $state<string | null>(null);
  let fName = $state('');
  let fEnabled = $state(true);
  let fTriggerKind = $state('event');
  let fEventType = $state('');
  let fCommand = $state('');
  let fIntervalMin = $state(10);
  let fWebhookSecret = $state('');
  let fWebhookHint = $state('');
  let fMode = $state('all');
  let fConds = $state<CondInstance[]>([]);
  let fActs = $state<ActInstance[]>([]);
  let fQueueId = $state('');
  // Raw JSON text mirrors for node configs without dedicated fields.
  let condRaw = $state<string[]>([]);
  let condRawErr = $state<boolean[]>([]);
  let actRaw = $state<string[]>([]);
  let actRawErr = $state<boolean[]>([]);

  // Runs inspector state
  let runsFor = $state<string | null>(null);
  let runs = $state<RunHeader[]>([]);
  let runsLoading = $state(false);
  let runDetail = $state<RunDetail | null>(null);
  let runDetailLoading = $state(false);
  let highlightRun = $state<string | null>(null);

  async function load() {
    if (!channel) { return; }
    loading = true;
    try {
      const [list, cat] = await Promise.all([
        channelApi(channel).get<ListResponse>('/actions'),
        channelApi(channel).get<Catalog>('/actions/catalog'),
      ]);
      rules = list.rules ?? [];
      catalog = { conditions: cat.conditions ?? [], actions: cat.actions ?? [], triggers: cat.triggers ?? [] };
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'The actions feature is not enabled.' : 'Could not load.', 'error');
    } finally {
      loading = false;
    }
  }

  function resetForm() {
    editing = null;
    fName = '';
    fEnabled = true;
    fTriggerKind = 'event';
    fEventType = '';
    fCommand = '';
    fIntervalMin = 10;
    fWebhookSecret = '';
    fWebhookHint = '';
    fMode = 'all';
    fConds = [];
    fActs = catalog.actions.length ? [{ type_id: catalog.actions[0].id, config: {}, enabled: true }] : [];
    fQueueId = '';
    condRaw = []; condRawErr = [];
    actRaw = fActs.map((a) => JSON.stringify(a.config)); actRawErr = fActs.map(() => false);
  }

  function openNew() {
    if (!catalog.actions.length) { toast('Load the catalog first.', 'warn'); return; }
    resetForm();
    showForm = true;
  }

  function openEdit(r: Rule) {
    editing = r.name;
    fName = r.name;
    fEnabled = r.enabled;
    fTriggerKind = r.trigger_kind || 'event';
    fEventType = (r.trigger_filter?.event_type as string) ?? '';
    fCommand = (r.trigger_filter?.command as string) ?? '';
    fIntervalMin = r.trigger_filter?.interval_seconds
      ? Math.max(1, Math.round((r.trigger_filter.interval_seconds as number) / 60))
      : 10;
    fWebhookSecret = '';
    fWebhookHint = (r.trigger_filter?.secret_set as boolean) ? ((r.trigger_filter?.secret_hint as string) || '…') : '';
    fMode = r.conditions?.mode || 'all';
    fConds = (r.conditions?.conditions ?? []).map((c) => ({ type_id: c.type_id, config: { ...(c.config ?? {}) } }));
    fActs = (r.actions?.actions ?? []).map((a) => ({ type_id: a.type_id, config: { ...(a.config ?? {}) }, enabled: a.enabled }));
    fQueueId = r.actions?.queue_id ?? '';
    condRaw = fConds.map((c) => JSON.stringify(c.config)); condRawErr = fConds.map(() => false);
    actRaw = fActs.map((a) => JSON.stringify(a.config)); actRawErr = fActs.map(() => false);
    showForm = true;
  }

  function addCond() {
    if (!catalog.conditions.length) return;
    fConds = [...fConds, { type_id: catalog.conditions[0].id, config: {} }];
    condRaw = [...condRaw, '{}']; condRawErr = [...condRawErr, false];
  }
  function removeCond(i: number) {
    fConds = fConds.filter((_, idx) => idx !== i);
    condRaw = condRaw.filter((_, idx) => idx !== i);
    condRawErr = condRawErr.filter((_, idx) => idx !== i);
  }

  function addAct() {
    if (!catalog.actions.length) return;
    fActs = [...fActs, { type_id: catalog.actions[0].id, config: {}, enabled: true }];
    actRaw = [...actRaw, '{}']; actRawErr = [...actRawErr, false];
  }
  function removeAct(i: number) {
    fActs = fActs.filter((_, idx) => idx !== i);
    actRaw = actRaw.filter((_, idx) => idx !== i);
    actRawErr = actRawErr.filter((_, idx) => idx !== i);
  }
  function moveAct(i: number, dir: -1 | 1) {
    const j = i + dir;
    if (j < 0 || j >= fActs.length) return;
    const next = [...fActs];
    [next[i], next[j]] = [next[j], next[i]];
    fActs = next;
    const nextRaw = [...actRaw];
    [nextRaw[i], nextRaw[j]] = [nextRaw[j], nextRaw[i]];
    actRaw = nextRaw;
    const nextErr = [...actRawErr];
    [nextErr[i], nextErr[j]] = [nextErr[j], nextErr[i]];
    actRawErr = nextErr;
  }

  // Node types with dedicated form fields; everything else edits raw JSON.
  const dedicatedConds = new Set(['builtin:message-contains', 'builtin:user-role', 'cond:regex']);
  const dedicatedActs = new Set([
    'builtin:send-chat', 'builtin:delay', 'builtin:log',
    'obs:switch-scene', 'obs:set-source-visibility',
    'transform:template', 'twitch:timeout',
  ]);

  function syncCondRaw(i: number, text: string) {
    condRaw[i] = text;
    try {
      const v = JSON.parse(text || '{}');
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        fConds[i].config = v as Record<string, unknown>;
        condRawErr[i] = false;
        return;
      }
    } catch { /* fallthrough */ }
    condRawErr[i] = true;
  }
  function syncActRaw(i: number, text: string) {
    actRaw[i] = text;
    try {
      const v = JSON.parse(text || '{}');
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        fActs[i].config = v as Record<string, unknown>;
        actRawErr[i] = false;
        return;
      }
    } catch { /* fallthrough */ }
    actRawErr[i] = true;
  }

  function buildTriggerFilter(): Record<string, unknown> | undefined {
    if (fTriggerKind === 'event') return fEventType.trim() ? { event_type: fEventType.trim() } : undefined;
    if (fTriggerKind === 'command') return { command: fCommand.trim() };
    if (fTriggerKind === 'timer') return { interval_seconds: Math.max(5, Math.round(fIntervalMin * 60)) };
    if (fTriggerKind === 'webhook') return fWebhookSecret.trim() ? { secret: fWebhookSecret.trim() } : undefined;
    return undefined;
  }

  function genSecret(): string {
    const b = new Uint8Array(32);
    crypto.getRandomValues(b);
    return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  }

  // Pre-generate a visible secret for new webhook rules so the operator can
  // copy it for the caller — after saving it is only ever shown masked.
  $effect(() => {
    if (showForm && fTriggerKind === 'webhook' && !fWebhookSecret && !fWebhookHint) {
      fWebhookSecret = genSecret();
    }
  });

  function webhookUrl(name: string): string {
    const base = typeof window !== 'undefined' ? window.location.origin : '';
    return `${base}/api/v1/channels/${encodeURIComponent(channel)}/webhooks/${encodeURIComponent(name)}`;
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast('Copied.', 'success');
    } catch {
      toast('Could not copy.', 'error');
    }
  }

  async function runNow(r: Rule) {
    if (firing) return;
    firing = r.name;
    try {
      const res = await channelApi(channel).post<{ status: string; run_id?: string }>(`/actions/${encodeURIComponent(r.name)}/fire`);
      toast(`"${r.name}" fired.`, 'success');
      if (runsFor === r.name) {
        highlightRun = res.run_id ?? null;
        setTimeout(() => { void loadRuns(r.name); }, 1200);
      }
    } catch (err) {
      toast(err instanceof ApiException && err.status === 404 ? 'Rule not found.' : 'Could not fire the rule.', 'error');
    } finally {
      firing = null;
    }
  }

  async function save() {
    if (!channel || !fName.trim()) { toast('Name is required.', 'warn'); return; }
    if (fActs.length === 0) { toast('At least one action is required.', 'warn'); return; }
    if (fTriggerKind === 'command' && !fCommand.trim()) { toast('Command word is required.', 'warn'); return; }
    if (condRawErr.some(Boolean) || actRawErr.some(Boolean)) { toast('Fix invalid JSON config first.', 'warn'); return; }

    const body = {
      name: fName.trim(),
      enabled: fEnabled,
      trigger_kind: fTriggerKind,
      trigger_filter: buildTriggerFilter(),
      conditions: { mode: fMode, conditions: fConds },
      actions: { actions: fActs, queue_id: fQueueId.trim() || undefined },
    };
    try {
      if (editing) {
        const r = await channelApi(channel).put<Rule>(`/actions/${encodeURIComponent(editing)}`, body);
        rules = rules.map((x) => (x.name === editing ? r : x));
        toast('Rule updated.', 'success');
      } else {
        const r = await channelApi(channel).post<Rule>('/actions', body);
        rules = [...rules, r];
        toast('Rule created.', 'success');
      }
      showForm = false;
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) toast('Name already exists.', 'error');
      else if (err instanceof ApiException && err.status === 400) toast('Invalid rule.', 'error');
      else toast('Could not save.', 'error');
    }
  }

  async function toggle(r: Rule) {
    try {
      const u = await channelApi(channel).put<Rule>(`/actions/${encodeURIComponent(r.name)}`, {
        name: r.name, enabled: !r.enabled, trigger_kind: r.trigger_kind,
        trigger_filter: r.trigger_filter ?? undefined, conditions: r.conditions, actions: r.actions,
      });
      rules = rules.map((x) => (x.name === r.name ? u : x));
    } catch {
      toast('Could not update.', 'error');
    }
  }

  async function del(name: string) {
    try {
      await channelApi(channel).delete(`/actions/${encodeURIComponent(name)}`);
      rules = rules.filter((x) => x.name !== name);
      if (runsFor === name) closeRuns();
      toast('Rule deleted.', 'warn');
    } catch {
      toast('Could not delete.', 'error');
    }
  }

  // --- Runs inspector ---

  async function openRuns(name: string) {
    runsFor = name;
    runDetail = null;
    highlightRun = null;
    await loadRuns(name);
  }

  function closeRuns() {
    runsFor = null;
    runs = [];
    runDetail = null;
    highlightRun = null;
  }

  async function loadRuns(name: string) {
    runsLoading = true;
    try {
      const res = await channelApi(channel).get<RunsResponse>(`/actions/${encodeURIComponent(name)}/runs?limit=50`);
      runs = res.runs ?? [];
    } catch (err) {
      runs = [];
      toast(err instanceof ApiException && err.status === 501 ? 'Run history is not enabled.' : 'Could not load runs.', 'error');
    } finally {
      runsLoading = false;
    }
  }

  async function openRunDetail(name: string, runID: string) {
    runDetailLoading = true;
    try {
      runDetail = await channelApi(channel).get<RunDetail>(`/actions/${encodeURIComponent(name)}/runs/${encodeURIComponent(runID)}`);
    } catch {
      toast('Could not load the run.', 'error');
    } finally {
      runDetailLoading = false;
    }
  }

  async function clearRuns(name: string) {
    try {
      await channelApi(channel).delete(`/actions/${encodeURIComponent(name)}/runs`);
      runs = [];
      runDetail = null;
      toast('Run history cleared.', 'warn');
    } catch {
      toast('Could not clear the history.', 'error');
    }
  }

  const condName = (id: string) => catalog.conditions.find((c) => c.id === id)?.name ?? id;
  const actName = (id: string) => catalog.actions.find((a) => a.id === id)?.name ?? id;

  function triggerSummary(r: Rule): string {
    if (r.trigger_kind === 'command') return `Command !${(r.trigger_filter?.command as string)?.replace(/^!/, '') ?? '?'}`;
    if (r.trigger_kind === 'event') {
      const ev = r.trigger_filter?.event_type as string;
      return ev ? `Event ${ev}` : 'Any message';
    }
    if (r.trigger_kind === 'timer') {
      const sec = (r.trigger_filter?.interval_seconds as number) ?? 0;
      return sec >= 60 ? `Timer every ${Math.round(sec / 60)} min` : `Timer every ${sec} s`;
    }
    if (r.trigger_kind === 'webhook') return 'Inbound webhook';
    if (r.trigger_kind === 'manual') return 'Manual';
    return r.trigger_kind;
  }

  function fmtTime(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }

  function runDuration(r: RunHeader): string {
    const a = new Date(r.started_at).getTime();
    const b = new Date(r.finished_at).getTime();
    if (Number.isNaN(a) || Number.isNaN(b) || b < a) return '—';
    const ms = b - a;
    return ms >= 1000 ? `${(ms / 1000).toFixed(2)} s` : `${ms} ms`;
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; closeRuns(); void load(); }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="actions">
  <div class="page-wrap">
    <div class="toolbar">
      <span class="ws-label">{channel ? `@${channel}` : 'No workspace selected'}</span>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading || !channel}>{loading ? 'Loading…' : 'Reload'}</button>
      <div class="grow"></div>
      <span class="count-pill"><b>{rules.length}</b> rules</span>
      <a class="btn btn-ghost btn-sm" href="/actions/canvas">Canvas</a>
      <button class="btn btn-primary btn-sm" onclick={openNew} disabled={!channel}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>New rule</button>
    </div>

    {#if showForm}
      <div class="form-card">
        <div class="form-grid">
          <div><label class="fld" for="action-name">Name</label><div class="input"><input id="action-name" type="text" placeholder="e.g. discord-link" bind:value={fName} disabled={editing !== null} /></div></div>
          <label class="enable-row"><input type="checkbox" bind:checked={fEnabled} /> Enabled</label>
        </div>

        <div class="builder-block">
          <div class="block-head"><span class="badge tr">Trigger</span><span class="muted">What fires this rule?</span></div>
          <div class="form-grid">
            <div>
              <label class="fld" for="action-trigger-kind">Type</label>
              <div class="input"><select id="action-trigger-kind" bind:value={fTriggerKind}>
                {#each triggerOptions as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
              </select></div>
            </div>
            {#if fTriggerKind === 'event'}
              <div><label class="fld" for="action-event-type">Event type (empty = any message)</label><div class="input"><input id="action-event-type" type="text" placeholder="e.g. channel.raided" bind:value={fEventType} /></div></div>
            {:else if fTriggerKind === 'command'}
              <div><label class="fld" for="action-command">Command word</label><div class="input"><input id="action-command" type="text" placeholder="e.g. !discord" bind:value={fCommand} /></div></div>
            {:else if fTriggerKind === 'timer'}
              <div><label class="fld" for="action-interval">Interval (minutes)</label><div class="input"><input id="action-interval" type="number" min="1" step="1" bind:value={fIntervalMin} /></div><div class="muted small">At least 5 seconds between firings.</div></div>
            {:else if fTriggerKind === 'manual'}
              <div class="muted small">This rule only runs when you fire it manually.</div>
            {/if}
          </div>
          {#if fTriggerKind === 'webhook'}
            <div class="webhook-box">
              {#if fName.trim()}
                <div class="hook-row">
                  <span class="fld">Endpoint</span>
                  <code class="hook-url">{webhookUrl(fName.trim())}</code>
                  <button class="btn btn-ghost btn-sm" onclick={() => copyText(webhookUrl(fName.trim()))}>Copy</button>
                </div>
              {:else}
                <div class="muted small">Name the rule to see its endpoint URL.</div>
              {/if}
              <div class="form-grid">
                <div>
                  <label class="fld" for="action-webhook-secret">HMAC secret {#if fWebhookHint}<span class="muted">(set, hint {fWebhookHint} — leave empty to keep)</span>{/if}</label>
                  <div class="hook-row">
                    <div class="input grow"><input id="action-webhook-secret" type="text" placeholder={fWebhookHint ? 'Unchanged' : ''} bind:value={fWebhookSecret} autocomplete="off" spellcheck="false" /></div>
                    {#if fWebhookSecret}<button class="btn btn-ghost btn-sm" onclick={() => copyText(fWebhookSecret)}>Copy</button>{/if}
                    {#if fWebhookHint}<button class="btn btn-ghost btn-sm" onclick={() => (fWebhookSecret = genSecret())}>Rotate</button>{/if}
                  </div>
                </div>
              </div>
              <div class="muted small">Callers must sign the raw body: <code>X-Engelos-Signature: hex(HMAC-SHA256(body, secret))</code>. The secret is shown only here, never again after saving.</div>
            </div>
          {/if}
        </div>

        <div class="builder-block">
          <div class="block-head">
            <span class="badge co">Conditions</span>
            <div class="input" style="max-width:130px"><select bind:value={fMode}>
              <option value="all">All (AND)</option>
              <option value="any">Any (OR)</option>
              <option value="none">None</option>
            </select></div>
            <div class="grow"></div>
            <button class="btn btn-ghost btn-sm" onclick={addCond} disabled={!catalog.conditions.length}>+ Condition</button>
          </div>
          {#if fConds.length === 0}<div class="muted small">No conditions: the rule always fires.</div>{/if}
          {#each fConds as c, i (i)}
            <div class="row-item">
              <div class="input" style="min-width:180px"><select bind:value={c.type_id}>
                {#each catalog.conditions as cd (cd.id)}<option value={cd.id}>{cd.name}</option>{/each}
              </select></div>
              {#if c.type_id === 'builtin:message-contains'}
                <div class="input grow"><input type="text" placeholder="Text that must be present" value={(c.config.substring as string) ?? ''} oninput={(e) => (c.config.substring = e.currentTarget.value)} /></div>
                <label class="enable-row small"><input type="checkbox" checked={(c.config.case_sensitive as boolean) ?? false} onchange={(e) => (c.config.case_sensitive = e.currentTarget.checked)} /> Case-sensitive</label>
              {:else if c.type_id === 'builtin:user-role'}
                <div class="input grow"><select value={(c.config.role as string) ?? 'everyone'} onchange={(e) => (c.config.role = e.currentTarget.value)}>
                  <option value="everyone">Everyone</option>
                  <option value="subscriber">Subscriber</option>
                  <option value="vip">VIP</option>
                  <option value="moderator">Moderator</option>
                  <option value="broadcaster">Broadcaster</option>
                </select></div>
              {:else if c.type_id === 'cond:regex'}
                <div class="input grow"><input type="text" placeholder="Pattern, e.g. ^!raffle\s+join$" value={(c.config.pattern as string) ?? ''} oninput={(e) => (c.config.pattern = e.currentTarget.value)} /></div>
                <label class="enable-row small"><input type="checkbox" checked={(c.config.case_insensitive as boolean) ?? false} onchange={(e) => (c.config.case_insensitive = e.currentTarget.checked)} /> Ignore case</label>
              {:else}
                <div class="input grow json-input" class:invalid={condRawErr[i]}>
                  <input type="text" placeholder={'JSON config, e.g. {"state":"live"}'} value={condRaw[i] ?? '{}'} oninput={(e) => syncCondRaw(i, e.currentTarget.value)} spellcheck="false" />
                </div>
              {/if}
              <button class="iact del" onclick={() => removeCond(i)} aria-label="Remove"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14" /></svg></button>
            </div>
          {/each}
        </div>

        <div class="builder-block">
          <div class="block-head">
            <span class="badge ac">Actions</span><span class="muted">Run in order, top to bottom</span>
            <div class="grow"></div>
            <button class="btn btn-ghost btn-sm" onclick={addAct} disabled={!catalog.actions.length}>+ Action</button>
          </div>
          {#each fActs as a, i (i)}
            <div class="row-item">
              <span class="ord">{i + 1}</span>
              <div class="input" style="min-width:170px"><select bind:value={a.type_id}>
                {#each catalog.actions as ad (ad.id)}<option value={ad.id}>{ad.name}</option>{/each}
              </select></div>
              {#if a.type_id === 'builtin:send-chat'}
                <div class="input grow"><input type="text" placeholder="Message, e.g. Hi $(user)!" value={(a.config.text as string) ?? ''} oninput={(e) => (a.config.text = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'builtin:delay'}
                <div class="input" style="max-width:130px"><input type="number" min="0" step="0.5" placeholder="Seconds" value={(a.config.seconds as number) ?? 0} oninput={(e) => (a.config.seconds = parseFloat(e.currentTarget.value) || 0)} /></div>
              {:else if a.type_id === 'builtin:log'}
                <div class="input grow"><input type="text" placeholder="Log text" value={(a.config.message as string) ?? ''} oninput={(e) => (a.config.message = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'obs:switch-scene'}
                <div class="input grow"><input type="text" placeholder="Scene name, e.g. Starting Soon" value={(a.config.scene as string) ?? ''} oninput={(e) => (a.config.scene = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'obs:set-source-visibility'}
                <div class="input" style="min-width:150px"><input type="text" placeholder="Scene" value={(a.config.scene as string) ?? ''} oninput={(e) => (a.config.scene = e.currentTarget.value)} /></div>
                <div class="input grow"><input type="text" placeholder="Source name" value={(a.config.source as string) ?? ''} oninput={(e) => (a.config.source = e.currentTarget.value)} /></div>
                <label class="enable-row small"><input type="checkbox" checked={(a.config.visible as boolean) ?? false} onchange={(e) => (a.config.visible = e.currentTarget.checked)} /> Visible</label>
              {:else if a.type_id === 'transform:template'}
                <div class="input grow"><input type="text" placeholder={'Template, e.g. New follower: $(user)'} value={(a.config.template as string) ?? ''} oninput={(e) => (a.config.template = e.currentTarget.value)} /></div>
                <div class="input" style="max-width:140px"><input type="text" placeholder="Output key" value={(a.config.output_key as string) ?? ''} oninput={(e) => (a.config.output_key = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'twitch:timeout'}
                <div class="input" style="max-width:150px"><input type="number" min="1" step="1" placeholder="Seconds" value={(a.config.duration_seconds as number) ?? 600} oninput={(e) => (a.config.duration_seconds = parseInt(e.currentTarget.value, 10) || 600)} /></div>
                <div class="input grow"><input type="text" placeholder="Reason (optional)" value={(a.config.reason as string) ?? ''} oninput={(e) => (a.config.reason = e.currentTarget.value)} /></div>
              {:else}
                <div class="input grow json-input" class:invalid={actRawErr[i]}>
                  <input type="text" placeholder={'JSON config, e.g. {"url":"https://…"}'} value={actRaw[i] ?? '{}'} oninput={(e) => syncActRaw(i, e.currentTarget.value)} spellcheck="false" />
                </div>
              {/if}
              <div class="ord-btns">
                <button class="iact" onclick={() => moveAct(i, -1)} disabled={i === 0} aria-label="Move up"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6" /></svg></button>
                <button class="iact" onclick={() => moveAct(i, 1)} disabled={i === fActs.length - 1} aria-label="Move down"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6" /></svg></button>
              </div>
              <button class="iact del" onclick={() => removeAct(i)} aria-label="Remove"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14" /></svg></button>
            </div>
          {/each}
        </div>

        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Cancel</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Save</button>
        </div>
      </div>
    {/if}

    {#if runsFor}
      <div class="runs-card">
        <div class="runs-head">
          <span class="badge ru">Runs</span>
          <b>{runsFor}</b>
          <span class="muted small">last {runs.length} firings</span>
          <div class="grow"></div>
          <button class="btn btn-ghost btn-sm" onclick={() => runsFor && loadRuns(runsFor)} disabled={runsLoading}>{runsLoading ? 'Loading…' : 'Refresh'}</button>
          <button class="btn btn-ghost btn-sm danger" onclick={() => runsFor && clearRuns(runsFor)} disabled={!runs.length}>Clear history</button>
          <button class="iact" onclick={closeRuns} aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
        </div>

        {#if runs.length === 0 && !runsLoading}
          <div class="muted small runs-empty">No recorded runs yet. Runs appear here after the rule fires and passes its conditions.</div>
        {:else}
          <div class="runs-split">
            <div class="runs-list">
              {#each runs as run (run.id)}
                <button
                  class="run-row"
                  class:active={runDetail?.id === run.id}
                  class:hot={highlightRun === run.id}
                  onclick={() => runsFor && openRunDetail(runsFor, run.id)}
                >
                  <span class="status-dot {run.status}"></span>
                  <span class="run-when">{fmtTime(run.started_at)}</span>
                  <span class="run-trigger muted">{run.trigger_summary || run.trigger_kind}</span>
                  <span class="grow"></span>
                  <span class="run-meta muted">{run.node_count} nodes · {runDuration(run)}</span>
                </button>
              {/each}
            </div>
            <div class="run-detail">
              {#if runDetailLoading}
                <div class="muted small">Loading…</div>
              {:else if runDetail}
                <div class="detail-head">
                  <span class="status-pill {runDetail.status}">{runDetail.status}</span>
                  <span class="muted small">{fmtTime(runDetail.started_at)} · {runDuration(runDetail)} · id <code>{runDetail.id}</code></span>
                </div>
                <ol class="trace">
                  {#each runDetail.nodes ?? [] as n (n.seq)}
                    <li class="trace-node {n.status}">
                      <div class="trace-top">
                        <span class="kind-badge {n.node_kind}">{n.node_kind === 'condition' ? 'IF' : 'DO'}</span>
                        <code class="type-id">{n.type_id}</code>
                        <span class="grow"></span>
                        <span class="status-pill sm {n.status}">{n.status}</span>
                        <span class="muted small">{n.duration_ms} ms</span>
                      </div>
                      {#if n.error}<div class="trace-err">{n.error}</div>{/if}
                      {#if n.output_summary}<pre class="trace-out">{n.output_summary}</pre>{/if}
                    </li>
                  {/each}
                </ol>
              {:else}
                <div class="muted small">Select a run on the left to inspect its node-by-node trace.</div>
              {/if}
            </div>
          </div>
        {/if}
      </div>
    {/if}

    <table class="dtable" style="margin-top:14px">
      <thead><tr><th>Name</th><th>Trigger</th><th class="right" style="width:90px">Actions</th><th style="width:90px">Active</th><th class="right" style="width:160px">Action</th></tr></thead>
      <tbody>
        {#each rules as r (r.name)}
          <tr>
            <td><b>{r.name}</b></td>
            <td class="muted">{triggerSummary(r)}</td>
            <td class="right num">{r.actions?.actions?.length ?? 0}</td>
            <td><button class="switch" class:on={r.enabled} onclick={() => toggle(r)} aria-label="Toggle"></button></td>
            <td>
              <div class="row-actions">
                <button class="iact" onclick={() => runNow(r)} disabled={firing === r.name} aria-label="Fire" title="Fire now"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3l14 9-14 9z" /></svg></button>
                <button class="iact" class:active={runsFor === r.name} onclick={() => (runsFor === r.name ? closeRuns() : openRuns(r.name))} aria-label="Run history" title="Run history"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 3" /></svg></button>
                <a class="iact" href={`/actions/canvas?rule=${encodeURIComponent(r.name)}`} aria-label="Open in canvas" title="Open in canvas"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="12" r="2.5"/><path d="M7.5 6H12a4 4 0 0 1 4 4v.5M7.5 18H12a4 4 0 0 0 4-4v-.5"/></svg></a>
                <button class="iact" onclick={() => openEdit(r)} aria-label="Edit"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg></button>
                <button class="iact del" onclick={() => del(r.name)} aria-label="Delete"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if rules.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="12" r="2.5"/><path d="M7.5 6H12a4 4 0 0 1 4 4v.5M7.5 18H12a4 4 0 0 0 4-4v-.5"/></svg>
        <div class="t">{channel ? 'No rules yet' : 'Select a workspace above'}</div>
        <div class="d">Build automations: triggers, conditions, actions.</div>
      </div>
    {/if}
  </div>
</section>

<style>
  .form-card { display: flex; flex-direction: column; gap: 16px; margin: 8px 0 4px; padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; align-items: end; }
  .form-actions { display: flex; gap: 10px; justify-content: flex-end; }
  .enable-row { display: flex; align-items: center; gap: 9px; font-size: .9rem; color: var(--text-dim); font-weight: 600; }
  .enable-row.small { font-size: .8rem; white-space: nowrap; }
  .builder-block { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: var(--radius); background: var(--bg); border: 1px solid var(--panel-border); }
  .block-head { display: flex; align-items: center; gap: 10px; }
  .badge { font-size: .72rem; font-weight: 800; text-transform: uppercase; letter-spacing: .04em; padding: 3px 9px; border-radius: 999px; color: #fff; }
  .badge.tr { background: var(--brand); }
  .badge.co { background: var(--brand-2); }
  .badge.ac { background: linear-gradient(135deg, var(--brand), var(--brand-2)); }
  .badge.ru { background: linear-gradient(135deg, var(--brand-2), var(--brand)); }
  .row-item { display: flex; align-items: center; gap: 8px; }
  .ord { width: 22px; height: 22px; flex: 0 0 auto; display: grid; place-items: center; font-size: .78rem; font-weight: 800; color: var(--text-dim); background: var(--panel-bg); border: 1px solid var(--panel-border); border-radius: 7px; }
  .ord-btns { display: flex; gap: 2px; }
  .grow { flex: 1 1 auto; }
  .muted { color: var(--text-dim); }
  .small { font-size: .82rem; }
  .ws-label { font-weight: 700; color: var(--text-dim); font-size: .9rem; }
  .json-input input { font-family: var(--font-mono, ui-monospace, monospace); font-size: .82rem; }
  .json-input.invalid { outline: 1px solid var(--danger, #e5484d); border-radius: var(--radius-sm, 8px); }
  .webhook-box { display: flex; flex-direction: column; gap: 10px; padding: 12px; border-radius: var(--radius); background: var(--panel-bg); border: 1px dashed var(--panel-border); }
  .hook-row { display: flex; align-items: center; gap: 10px; min-width: 0; }
  .hook-url { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .8rem; padding: 6px 10px; background: var(--bg); border: 1px solid var(--panel-border); border-radius: 8px; }

  /* Runs inspector */
  .runs-card { display: flex; flex-direction: column; gap: 12px; margin: 8px 0 4px; padding: 16px 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .runs-head { display: flex; align-items: center; gap: 10px; }
  .runs-empty { padding: 8px 2px; }
  .runs-split { display: grid; grid-template-columns: minmax(260px, 340px) 1fr; gap: 14px; align-items: start; }
  .runs-list { display: flex; flex-direction: column; gap: 4px; max-height: 380px; overflow-y: auto; padding-right: 4px; }
  .run-row { display: flex; align-items: center; gap: 8px; width: 100%; text-align: left; padding: 8px 10px; border-radius: 10px; background: var(--bg); border: 1px solid var(--panel-border); cursor: pointer; color: inherit; font: inherit; }
  .run-row:hover { border-color: var(--brand); }
  .run-row.active { border-color: var(--brand); background: color-mix(in srgb, var(--brand) 8%, var(--bg)); }
  .run-row.hot { border-color: var(--brand-2); }
  .run-when { font-size: .82rem; font-weight: 700; white-space: nowrap; }
  .run-trigger { font-size: .78rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 120px; }
  .run-meta { font-size: .75rem; white-space: nowrap; }
  .status-dot { width: 9px; height: 9px; flex: 0 0 auto; border-radius: 999px; background: var(--text-dim); }
  .status-dot.ok { background: var(--success, #30a46c); }
  .status-dot.partial { background: var(--warning, #f5a524); }
  .status-dot.error { background: var(--danger, #e5484d); }
  .status-dot.stopped { background: var(--brand-2); }
  .run-detail { min-width: 0; display: flex; flex-direction: column; gap: 10px; padding: 12px; border-radius: 10px; background: var(--bg); border: 1px solid var(--panel-border); min-height: 120px; }
  .detail-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .status-pill { font-size: .72rem; font-weight: 800; text-transform: uppercase; letter-spacing: .04em; padding: 3px 10px; border-radius: 999px; color: #fff; background: var(--text-dim); }
  .status-pill.sm { font-size: .66rem; padding: 2px 8px; }
  .status-pill.ok { background: var(--success, #30a46c); }
  .status-pill.partial { background: var(--warning, #f5a524); }
  .status-pill.error { background: var(--danger, #e5484d); }
  .status-pill.stopped { background: var(--brand-2); }
  .trace { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 0; list-style: none; }
  .trace-node { display: flex; flex-direction: column; gap: 6px; padding: 10px 12px; border-radius: 10px; background: var(--panel-bg); border: 1px solid var(--panel-border); border-left-width: 3px; }
  .trace-node.ok { border-left-color: var(--success, #30a46c); }
  .trace-node.fail { border-left-color: var(--danger, #e5484d); }
  .trace-node.skipped, .trace-node.stopped { border-left-color: var(--text-dim); opacity: .75; }
  .trace-top { display: flex; align-items: center; gap: 8px; }
  .kind-badge { font-size: .64rem; font-weight: 800; letter-spacing: .06em; padding: 2px 7px; border-radius: 6px; color: #fff; background: var(--brand-2); }
  .kind-badge.action { background: var(--brand); }
  .type-id { font-size: .8rem; }
  .trace-err { font-size: .8rem; color: var(--danger, #e5484d); word-break: break-word; }
  .trace-out { margin: 0; padding: 8px 10px; font-size: .74rem; line-height: 1.5; background: var(--bg); border: 1px solid var(--panel-border); border-radius: 8px; overflow-x: auto; white-space: pre-wrap; word-break: break-word; max-height: 140px; overflow-y: auto; }
  .iact.active { color: var(--brand); }
  .btn.danger { color: var(--danger, #e5484d); }

  @media (max-width: 900px) { .runs-split { grid-template-columns: 1fr; } }
  @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } .row-item { flex-wrap: wrap; } }
</style>
