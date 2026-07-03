<script lang="ts">
  import { SvelteFlow, Background, Controls, MiniMap, type Node, type Edge } from '@xyflow/svelte';
  import '@xyflow/svelte/dist/style.css';
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import CanvasNode from './CanvasNode.svelte';

  type CondInstance = { type_id: string; config: Record<string, unknown> };
  type ActInstance = { type_id: string; config: Record<string, unknown>; enabled: boolean };
  type Rule = {
    name: string;
    enabled: boolean;
    trigger_kind: string;
    trigger_filter: Record<string, unknown> | null;
    conditions: { mode: string; conditions: CondInstance[] };
    actions: { actions: ActInstance[]; queue_id?: string };
  };
  type ListResponse = { channel: string; rules: Rule[] };
  type PluginDef = { id: string; name: string; description: string };
  type Catalog = { conditions: PluginDef[]; actions: PluginDef[] };

  const nodeTypes = { engel: CanvasNode };

  let channel = $state('');
  let rules = $state<Rule[]>([]);
  let catalog = $state<Catalog>({ conditions: [], actions: [] });
  let selected = $state('');
  let nodes = $state.raw<Node[]>([]);
  let edges = $state.raw<Edge[]>([]);

  // --- Editor state (mirrors the rule being edited) ---
  let creating = $state(false);
  let eName = $state('');
  let eEnabled = $state(true);
  let eTriggerKind = $state('event');
  let eEventType = $state('');
  let eCommand = $state('');
  let eIntervalMin = $state(10);
  let eWebhookSecret = $state('');
  let eWebhookHint = $state('');
  let eMode = $state('all');
  let eConds = $state<CondInstance[]>([]);
  let eActs = $state<ActInstance[]>([]);
  let eQueueId = $state('');
  let dirty = $state(false);
  let saving = $state(false);

  // Selection: 'trigger' | 'cond-N' | 'act-N' | null
  let sel = $state<string | null>(null);
  // Raw JSON mirror for the inspector's fallback editor.
  let rawText = $state('');
  let rawErr = $state(false);

  const triggerKinds = [
    { id: 'event', name: 'Event / Message' },
    { id: 'command', name: 'Command' },
    { id: 'timer', name: 'Timer / Interval' },
    { id: 'webhook', name: 'Inbound Webhook' },
    { id: 'manual', name: 'Manual' },
  ];

  async function load() {
    if (!channel) return;
    try {
      const [list, cat] = await Promise.all([
        channelApi(channel).get<ListResponse>('/actions'),
        channelApi(channel).get<Catalog>('/actions/catalog'),
      ]);
      rules = list.rules ?? [];
      catalog = { conditions: cat.conditions ?? [], actions: cat.actions ?? [] };
      const wanted = $page.url.searchParams.get('rule');
      if (wanted && rules.some((r) => r.name === wanted)) {
        selected = wanted;
      } else if (rules.length && !rules.some((r) => r.name === selected)) {
        selected = rules[0].name;
      }
      if (selected) loadRule(selected);
      else startNew();
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'The actions feature is not enabled.' : 'Could not load.', 'error');
    }
  }

  function loadRule(name: string) {
    const r = rules.find((x) => x.name === name);
    if (!r) return;
    creating = false;
    eName = r.name;
    eEnabled = r.enabled;
    eTriggerKind = r.trigger_kind || 'event';
    const f = r.trigger_filter ?? {};
    eEventType = (f.event_type as string) ?? '';
    eCommand = (f.command as string) ?? '';
    eIntervalMin = f.interval_seconds ? Math.max(1, Math.round((f.interval_seconds as number) / 60)) : 10;
    eWebhookSecret = '';
    eWebhookHint = (f.secret_set as boolean) ? ((f.secret_hint as string) || '…') : '';
    eMode = r.conditions?.mode || 'all';
    eConds = (r.conditions?.conditions ?? []).map((c) => ({ type_id: c.type_id, config: { ...(c.config ?? {}) } }));
    eActs = (r.actions?.actions ?? []).map((a) => ({ type_id: a.type_id, config: { ...(a.config ?? {}) }, enabled: a.enabled }));
    eQueueId = r.actions?.queue_id ?? '';
    dirty = false;
    sel = null;
    build();
  }

  function startNew() {
    creating = true;
    selected = '';
    eName = '';
    eEnabled = true;
    eTriggerKind = 'event';
    eEventType = '';
    eCommand = '';
    eIntervalMin = 10;
    eWebhookSecret = '';
    eWebhookHint = '';
    eMode = 'all';
    eConds = [];
    eActs = [];
    eQueueId = '';
    dirty = false;
    sel = 'trigger';
    syncRaw();
    build();
  }

  const condName = (id: string) => catalog.conditions.find((c) => c.id === id)?.name ?? id;
  const actName = (id: string) => catalog.actions.find((a) => a.id === id)?.name ?? id;
  const condDesc = (id: string) => catalog.conditions.find((c) => c.id === id)?.description ?? '';
  const actDesc = (id: string) => catalog.actions.find((a) => a.id === id)?.description ?? '';

  function triggerLabel(): { title: string; subtitle: string } {
    switch (eTriggerKind) {
      case 'command': return { title: `Command !${(eCommand || '?').replace(/^!/, '')}`, subtitle: 'chat command' };
      case 'event': return { title: eEventType || 'Any message', subtitle: 'platform event' };
      case 'timer': return { title: `Every ${eIntervalMin} min`, subtitle: 'timer' };
      case 'webhook': return { title: 'Inbound webhook', subtitle: 'HMAC-signed POST' };
      case 'manual': return { title: 'Manual fire', subtitle: 'dashboard only' };
      default: return { title: eTriggerKind, subtitle: '' };
    }
  }

  function configHint(config: Record<string, unknown>): string {
    const keys = Object.keys(config ?? {});
    if (!keys.length) return '';
    const k = keys[0];
    const v = config[k];
    const s = typeof v === 'string' ? v : JSON.stringify(v);
    return `${k}: ${s.length > 34 ? s.slice(0, 34) + '…' : s}`;
  }

  function build() {
    const n: Node[] = [];
    const e: Edge[] = [];
    const colGap = 320;
    const rowGap = 96;

    const t = triggerLabel();
    const condCenter = ((eConds.length - 1) * rowGap) / 2;
    const actCenter = ((eActs.length - 1) * rowGap) / 2;
    const midY = Math.max(condCenter, actCenter, 0);

    n.push({
      id: 'trigger', type: 'engel', position: { x: 0, y: midY },
      data: { kind: 'trigger', title: t.title, subtitle: t.subtitle },
      selected: sel === 'trigger',
    });

    eConds.forEach((c, i) => {
      const id = `cond-${i}`;
      n.push({
        id, type: 'engel', position: { x: colGap, y: midY - condCenter + i * rowGap },
        data: { kind: 'condition', title: condName(c.type_id), subtitle: configHint(c.config) },
        selected: sel === id,
      });
      e.push({ id: `e-trigger-${id}`, source: 'trigger', target: id, animated: true });
    });

    eActs.forEach((a, i) => {
      const id = `act-${i}`;
      n.push({
        id, type: 'engel',
        position: { x: colGap * 2 + i * 40, y: midY - actCenter + i * rowGap },
        data: {
          kind: i === eActs.length - 1 ? 'action-last' : 'action',
          title: actName(a.type_id), subtitle: configHint(a.config), disabled: !a.enabled,
        },
        selected: sel === id,
      });
      if (i === 0) {
        if (eConds.length) {
          eConds.forEach((_, ci) => e.push({ id: `e-cond-${ci}-${id}`, source: `cond-${ci}`, target: id }));
        } else {
          e.push({ id: `e-trigger-${id}`, source: 'trigger', target: id, animated: true });
        }
      } else {
        e.push({ id: `e-act-${i - 1}-${id}`, source: `act-${i - 1}`, target: id });
      }
    });

    nodes = n;
    edges = e;
  }

  // --- Selection & inspector ---

  function selectNode(id: string | null) {
    sel = id;
    syncRaw();
    build();
  }

  function selInfo(): { kind: 'trigger' | 'cond' | 'act'; index: number } | null {
    if (!sel) return null;
    if (sel === 'trigger') return { kind: 'trigger', index: -1 };
    if (sel.startsWith('cond-')) return { kind: 'cond', index: Number(sel.slice(5)) };
    if (sel.startsWith('act-')) return { kind: 'act', index: Number(sel.slice(4)) };
    return null;
  }

  function selCond(): CondInstance | null {
    const s = selInfo();
    return s?.kind === 'cond' ? (eConds[s.index] ?? null) : null;
  }
  function selAct(): ActInstance | null {
    const s = selInfo();
    return s?.kind === 'act' ? (eActs[s.index] ?? null) : null;
  }

  function syncRaw() {
    const c = selCond();
    const a = selAct();
    rawText = JSON.stringify((c?.config ?? a?.config) ?? {}, null, 0);
    rawErr = false;
  }

  function applyRaw(text: string) {
    rawText = text;
    try {
      const v = JSON.parse(text || '{}');
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        const c = selCond();
        const a = selAct();
        if (c) c.config = v as Record<string, unknown>;
        if (a) a.config = v as Record<string, unknown>;
        rawErr = false;
        markDirty();
        return;
      }
    } catch { /* invalid */ }
    rawErr = true;
  }

  function markDirty() {
    dirty = true;
    build();
  }

  function setCondType(i: number, typeId: string) {
    eConds[i] = { type_id: typeId, config: {} };
    syncRaw();
    markDirty();
  }
  function setActType(i: number, typeId: string) {
    eActs[i] = { type_id: typeId, config: {}, enabled: eActs[i]?.enabled ?? true };
    syncRaw();
    markDirty();
  }

  function addCond(typeId: string) {
    eConds = [...eConds, { type_id: typeId, config: {} }];
    sel = `cond-${eConds.length - 1}`;
    syncRaw();
    markDirty();
  }
  function addAct(typeId: string) {
    eActs = [...eActs, { type_id: typeId, config: {}, enabled: true }];
    sel = `act-${eActs.length - 1}`;
    syncRaw();
    markDirty();
  }

  function deleteSelected() {
    const s = selInfo();
    if (!s) return;
    if (s.kind === 'cond') eConds = eConds.filter((_, i) => i !== s.index);
    else if (s.kind === 'act') eActs = eActs.filter((_, i) => i !== s.index);
    else return;
    sel = null;
    markDirty();
  }

  function moveAct(dir: -1 | 1) {
    const s = selInfo();
    if (s?.kind !== 'act') return;
    const j = s.index + dir;
    if (j < 0 || j >= eActs.length) return;
    const next = [...eActs];
    [next[s.index], next[j]] = [next[j], next[s.index]];
    eActs = next;
    sel = `act-${j}`;
    markDirty();
  }

  function genSecret(): string {
    const b = new Uint8Array(32);
    crypto.getRandomValues(b);
    return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  }

  $effect(() => {
    if (eTriggerKind === 'webhook' && !eWebhookSecret && !eWebhookHint) {
      eWebhookSecret = genSecret();
    }
  });

  function webhookUrl(): string {
    const base = typeof window !== 'undefined' ? window.location.origin : '';
    return `${base}/api/v1/channels/${encodeURIComponent(channel)}/webhooks/${encodeURIComponent(eName.trim())}`;
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast('Copied.', 'success');
    } catch {
      toast('Could not copy.', 'error');
    }
  }

  function buildTriggerFilter(): Record<string, unknown> | undefined {
    if (eTriggerKind === 'event') return eEventType.trim() ? { event_type: eEventType.trim() } : undefined;
    if (eTriggerKind === 'command') return { command: eCommand.trim() };
    if (eTriggerKind === 'timer') return { interval_seconds: Math.max(5, Math.round(eIntervalMin * 60)) };
    if (eTriggerKind === 'webhook') return eWebhookSecret.trim() ? { secret: eWebhookSecret.trim() } : undefined;
    return undefined;
  }

  async function save() {
    if (!channel || !eName.trim()) { toast('Name is required.', 'warn'); sel = 'trigger'; build(); return; }
    if (eActs.length === 0) { toast('At least one action is required.', 'warn'); return; }
    if (eTriggerKind === 'command' && !eCommand.trim()) { toast('Command word is required.', 'warn'); return; }
    if (rawErr) { toast('Fix invalid JSON config first.', 'warn'); return; }

    const body = {
      name: eName.trim(),
      enabled: eEnabled,
      trigger_kind: eTriggerKind,
      trigger_filter: buildTriggerFilter(),
      conditions: { mode: eMode, conditions: eConds },
      actions: { actions: eActs, queue_id: eQueueId.trim() || undefined },
    };
    saving = true;
    try {
      if (creating) {
        const r = await channelApi(channel).post<Rule>('/actions', body);
        rules = [...rules, r];
        selected = r.name;
        creating = false;
        toast('Rule created.', 'success');
      } else {
        const r = await channelApi(channel).put<Rule>(`/actions/${encodeURIComponent(selected)}`, body);
        rules = rules.map((x) => (x.name === selected ? r : x));
        toast('Rule saved.', 'success');
      }
      // Reload from the server response so masked fields (webhook secret) round-trip correctly.
      loadRule(selected);
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) toast('Name already exists.', 'error');
      else if (err instanceof ApiException && err.status === 400) toast('Invalid rule.', 'error');
      else toast('Could not save.', 'error');
    } finally {
      saving = false;
    }
  }

  function switchRule(name: string) {
    if (dirty && !confirm('Discard unsaved changes?')) return;
    selected = name;
    loadRule(name);
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="actions-canvas">
  <div class="page-wrap">
    <div class="toolbar">
      <a class="btn btn-ghost btn-sm" href="/actions">← List</a>
      <span class="ws-label">{channel ? `@${channel}` : 'No workspace selected'}</span>
      <div class="input" style="max-width:220px">
        <select value={creating ? '' : selected} onchange={(e) => switchRule(e.currentTarget.value)} disabled={!rules.length}>
          {#if creating}<option value="">— new rule —</option>{/if}
          {#each rules as r (r.name)}<option value={r.name}>{r.name}</option>{/each}
        </select>
      </div>
      <button class="btn btn-ghost btn-sm" onclick={() => { if (!dirty || confirm('Discard unsaved changes?')) startNew(); }}>New</button>
      <div class="grow"></div>
      {#if dirty}<span class="dirty-pill">Unsaved changes</span>{/if}
      <label class="enable-row"><input type="checkbox" bind:checked={eEnabled} onchange={markDirty} /> Enabled</label>
      <button class="btn btn-primary btn-sm" onclick={save} disabled={saving || (!dirty && !creating)}>{saving ? 'Saving…' : 'Save'}</button>
    </div>

    <div class="editor-grid">
      <aside class="palette">
        <div class="pal-head">Add condition</div>
        <div class="pal-list">
          {#each catalog.conditions as c (c.id)}
            <button class="pal-item cond" onclick={() => addCond(c.id)} title={c.description}>
              <span class="pal-kind">IF</span>{c.name}
            </button>
          {/each}
        </div>
        <div class="pal-head">Add action</div>
        <div class="pal-list">
          {#each catalog.actions as a (a.id)}
            <button class="pal-item act" onclick={() => addAct(a.id)} title={a.description}>
              <span class="pal-kind">DO</span>{a.name}
            </button>
          {/each}
        </div>
      </aside>

      <div class="canvas-shell">
        <SvelteFlow
          bind:nodes bind:edges {nodeTypes} fitView
          proOptions={{ hideAttribution: true }} minZoom={0.4} maxZoom={1.6}
          onnodeclick={({ node }) => selectNode(node.id)}
          onpaneclick={() => selectNode(null)}
        >
          <Background gap={22} />
          <Controls showLock={false} />
          <MiniMap pannable zoomable />
        </SvelteFlow>
      </div>

      <aside class="inspector">
        {#if sel === 'trigger'}
          <div class="ins-head"><span class="ins-badge trigger">WHEN</span> Trigger</div>
          <label class="fld" for="canvas-name">Rule name</label>
          <div class="input"><input id="canvas-name" type="text" placeholder="e.g. discord-link" bind:value={eName} oninput={markDirty} disabled={!creating} /></div>
          <label class="fld" for="canvas-trigger-kind">Trigger type</label>
          <div class="input"><select id="canvas-trigger-kind" bind:value={eTriggerKind} onchange={markDirty}>
            {#each triggerKinds as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
          </select></div>
          {#if eTriggerKind === 'event'}
            <label class="fld" for="canvas-event-type">Event type (empty = any message)</label>
            <div class="input"><input id="canvas-event-type" type="text" placeholder="e.g. channel.raided" bind:value={eEventType} oninput={markDirty} /></div>
          {:else if eTriggerKind === 'command'}
            <label class="fld" for="canvas-command">Command word</label>
            <div class="input"><input id="canvas-command" type="text" placeholder="e.g. !discord" bind:value={eCommand} oninput={markDirty} /></div>
          {:else if eTriggerKind === 'timer'}
            <label class="fld" for="canvas-interval">Interval (minutes)</label>
            <div class="input"><input id="canvas-interval" type="number" min="1" step="1" bind:value={eIntervalMin} oninput={markDirty} /></div>
          {:else if eTriggerKind === 'webhook'}
            {#if eName.trim()}
              <label class="fld" for="canvas-webhook-url">Endpoint</label>
              <div class="hook-row"><code id="canvas-webhook-url" class="hook-url">{webhookUrl()}</code><button class="btn btn-ghost btn-sm" onclick={() => copyText(webhookUrl())}>Copy</button></div>
            {/if}
            <label class="fld" for="canvas-webhook-secret">HMAC secret {#if eWebhookHint}<span class="muted">(set, hint {eWebhookHint})</span>{/if}</label>
            <div class="hook-row">
              <div class="input grow"><input id="canvas-webhook-secret" type="text" placeholder={eWebhookHint ? 'Unchanged' : ''} bind:value={eWebhookSecret} oninput={markDirty} autocomplete="off" spellcheck="false" /></div>
              {#if eWebhookSecret}<button class="btn btn-ghost btn-sm" onclick={() => copyText(eWebhookSecret)}>Copy</button>{/if}
              {#if eWebhookHint}<button class="btn btn-ghost btn-sm" onclick={() => { eWebhookSecret = genSecret(); markDirty(); }}>Rotate</button>{/if}
            </div>
            <div class="muted small">Sign the raw body: <code>X-Engelos-Signature: hex(HMAC-SHA256(body, secret))</code>. Shown only before saving.</div>
          {:else if eTriggerKind === 'manual'}
            <div class="muted small">This rule only runs when you fire it manually.</div>
          {/if}
          {#if eConds.length}
            <label class="fld" for="canvas-mode">Condition mode</label>
            <div class="input"><select id="canvas-mode" bind:value={eMode} onchange={markDirty}>
              <option value="all">All must match (AND)</option>
              <option value="any">Any may match (OR)</option>
              <option value="none">None may match</option>
            </select></div>
          {/if}
        {:else if selCond()}
          {@const c = selCond()!}
          {@const s = selInfo()!}
          <div class="ins-head"><span class="ins-badge condition">IF</span> Condition</div>
          <label class="fld" for="canvas-cond-type">Type</label>
          <div class="input"><select id="canvas-cond-type" value={c.type_id} onchange={(e) => setCondType(s.index, e.currentTarget.value)}>
            {#each catalog.conditions as cd (cd.id)}<option value={cd.id}>{cd.name}</option>{/each}
          </select></div>
          {#if condDesc(c.type_id)}<div class="muted small">{condDesc(c.type_id)}</div>{/if}
          {#if c.type_id === 'builtin:message-contains'}
            <label class="fld" for="ci-substring">Text that must be present</label>
            <div class="input"><input id="ci-substring" type="text" value={(c.config.substring as string) ?? ''} oninput={(e) => { c.config.substring = e.currentTarget.value; markDirty(); }} /></div>
            <label class="enable-row small"><input type="checkbox" checked={(c.config.case_sensitive as boolean) ?? false} onchange={(e) => { c.config.case_sensitive = e.currentTarget.checked; markDirty(); }} /> Case-sensitive</label>
          {:else if c.type_id === 'builtin:user-role'}
            <label class="fld" for="ci-role">Minimum role</label>
            <div class="input"><select id="ci-role" value={(c.config.role as string) ?? 'everyone'} onchange={(e) => { c.config.role = e.currentTarget.value; markDirty(); }}>
              <option value="everyone">Everyone</option>
              <option value="subscriber">Subscriber</option>
              <option value="vip">VIP</option>
              <option value="moderator">Moderator</option>
              <option value="broadcaster">Broadcaster</option>
            </select></div>
          {:else if c.type_id === 'cond:regex'}
            <label class="fld" for="ci-pattern">Pattern</label>
            <div class="input"><input id="ci-pattern" type="text" placeholder={'e.g. ^!raffle\\s+join$'} value={(c.config.pattern as string) ?? ''} oninput={(e) => { c.config.pattern = e.currentTarget.value; markDirty(); }} spellcheck="false" /></div>
            <label class="enable-row small"><input type="checkbox" checked={(c.config.case_insensitive as boolean) ?? false} onchange={(e) => { c.config.case_insensitive = e.currentTarget.checked; markDirty(); }} /> Ignore case</label>
          {:else}
            <label class="fld" for="ci-json">Config (JSON)</label>
            <textarea id="ci-json" class="json-area" class:invalid={rawErr} rows="5" value={rawText} oninput={(e) => applyRaw(e.currentTarget.value)} spellcheck="false"></textarea>
          {/if}
          <div class="ins-actions">
            <button class="btn btn-ghost btn-sm danger" onclick={deleteSelected}>Delete condition</button>
          </div>
        {:else if selAct()}
          {@const a = selAct()!}
          {@const s = selInfo()!}
          <div class="ins-head"><span class="ins-badge action">DO</span> Action {s.index + 1} of {eActs.length}</div>
          <label class="fld" for="canvas-act-type">Type</label>
          <div class="input"><select id="canvas-act-type" value={a.type_id} onchange={(e) => setActType(s.index, e.currentTarget.value)}>
            {#each catalog.actions as ad (ad.id)}<option value={ad.id}>{ad.name}</option>{/each}
          </select></div>
          {#if actDesc(a.type_id)}<div class="muted small">{actDesc(a.type_id)}</div>{/if}
          {#if a.type_id === 'builtin:send-chat'}
            <label class="fld" for="ai-text">Message</label>
            <div class="input"><input id="ai-text" type="text" placeholder="e.g. Hi $(user)!" value={(a.config.text as string) ?? ''} oninput={(e) => { a.config.text = e.currentTarget.value; markDirty(); }} /></div>
          {:else if a.type_id === 'builtin:delay'}
            <label class="fld" for="ai-seconds">Delay (seconds)</label>
            <div class="input"><input id="ai-seconds" type="number" min="0" step="0.5" value={(a.config.seconds as number) ?? 0} oninput={(e) => { a.config.seconds = parseFloat(e.currentTarget.value) || 0; markDirty(); }} /></div>
          {:else if a.type_id === 'builtin:log'}
            <label class="fld" for="ai-message">Log text</label>
            <div class="input"><input id="ai-message" type="text" value={(a.config.message as string) ?? ''} oninput={(e) => { a.config.message = e.currentTarget.value; markDirty(); }} /></div>
          {:else if a.type_id === 'obs:switch-scene'}
            <label class="fld" for="ai-scene">Scene name</label>
            <div class="input"><input id="ai-scene" type="text" placeholder="e.g. Starting Soon" value={(a.config.scene as string) ?? ''} oninput={(e) => { a.config.scene = e.currentTarget.value; markDirty(); }} /></div>
          {:else if a.type_id === 'transform:template'}
            <label class="fld" for="ai-template">Template</label>
            <div class="input"><input id="ai-template" type="text" placeholder="e.g. New follower: $(user)" value={(a.config.template as string) ?? ''} oninput={(e) => { a.config.template = e.currentTarget.value; markDirty(); }} /></div>
            <label class="fld" for="ai-outkey">Output key</label>
            <div class="input"><input id="ai-outkey" type="text" placeholder="text" value={(a.config.output_key as string) ?? ''} oninput={(e) => { a.config.output_key = e.currentTarget.value; markDirty(); }} /></div>
          {:else if a.type_id === 'twitch:timeout'}
            <label class="fld" for="ai-duration">Duration (seconds)</label>
            <div class="input"><input id="ai-duration" type="number" min="1" step="1" value={(a.config.duration_seconds as number) ?? 600} oninput={(e) => { a.config.duration_seconds = parseInt(e.currentTarget.value, 10) || 600; markDirty(); }} /></div>
            <label class="fld" for="ai-reason">Reason (optional)</label>
            <div class="input"><input id="ai-reason" type="text" value={(a.config.reason as string) ?? ''} oninput={(e) => { a.config.reason = e.currentTarget.value; markDirty(); }} /></div>
          {:else}
            <label class="fld" for="ai-json">Config (JSON)</label>
            <textarea id="ai-json" class="json-area" class:invalid={rawErr} rows="6" value={rawText} oninput={(e) => applyRaw(e.currentTarget.value)} spellcheck="false"></textarea>
          {/if}
          <label class="enable-row"><input type="checkbox" checked={a.enabled} onchange={(e) => { a.enabled = e.currentTarget.checked; markDirty(); }} /> Enabled</label>
          <div class="ins-actions">
            <button class="btn btn-ghost btn-sm" onclick={() => moveAct(-1)} disabled={selInfo()?.index === 0}>↑ Earlier</button>
            <button class="btn btn-ghost btn-sm" onclick={() => moveAct(1)} disabled={selInfo()?.index === eActs.length - 1}>↓ Later</button>
            <button class="btn btn-ghost btn-sm danger" onclick={deleteSelected}>Delete</button>
          </div>
        {:else}
          <div class="ins-empty">
            <div class="t">Nothing selected</div>
            <div class="d">Click a node on the canvas to configure it, or add nodes from the palette on the left.</div>
          </div>
        {/if}
      </aside>
    </div>
  </div>
</section>

<style>
  .editor-grid {
    display: grid;
    grid-template-columns: 210px 1fr 300px;
    gap: 12px;
    margin-top: 14px;
    height: calc(100vh - 210px);
    min-height: 460px;
  }
  .canvas-shell {
    border-radius: var(--radius);
    border: 1px solid var(--panel-border);
    overflow: hidden;
    background: var(--bg);
    min-width: 0;
  }
  .ws-label { font-weight: 700; color: var(--text-dim); font-size: .9rem; }
  .grow { flex: 1 1 auto; }
  .muted { color: var(--text-dim); }
  .small { font-size: .8rem; }
  .enable-row { display: flex; align-items: center; gap: 8px; font-size: .86rem; color: var(--text-dim); font-weight: 600; white-space: nowrap; }
  .enable-row.small { font-size: .78rem; }
  .dirty-pill { font-size: .74rem; font-weight: 700; color: var(--warning, #f5a524); border: 1px solid color-mix(in srgb, var(--warning, #f5a524) 45%, transparent); padding: 3px 10px; border-radius: 999px; white-space: nowrap; }

  /* Palette */
  .palette { display: flex; flex-direction: column; gap: 8px; padding: 12px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); overflow-y: auto; }
  .pal-head { font-size: .7rem; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; color: var(--text-dim); margin-top: 4px; }
  .pal-list { display: flex; flex-direction: column; gap: 4px; }
  .pal-item { display: flex; align-items: center; gap: 8px; width: 100%; text-align: left; font: inherit; font-size: .8rem; font-weight: 600; color: inherit; padding: 7px 9px; border-radius: 9px; background: var(--bg); border: 1px solid var(--panel-border); cursor: pointer; }
  .pal-item:hover { border-color: var(--brand); }
  .pal-item.cond:hover { border-color: var(--brand-2); }
  .pal-kind { flex: 0 0 auto; font-size: .58rem; font-weight: 800; letter-spacing: .05em; padding: 2px 6px; border-radius: 5px; color: #fff; background: var(--brand); }
  .pal-item.cond .pal-kind { background: var(--brand-2); }

  /* Inspector */
  .inspector { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); overflow-y: auto; }
  .ins-head { display: flex; align-items: center; gap: 9px; font-weight: 800; font-size: .95rem; padding-bottom: 6px; border-bottom: 1px solid var(--panel-border); }
  .ins-badge { font-size: .62rem; font-weight: 800; letter-spacing: .06em; padding: 3px 8px; border-radius: 6px; color: #fff; background: var(--brand); }
  .ins-badge.condition { background: var(--brand-2); }
  .ins-badge.trigger { background: linear-gradient(135deg, var(--brand), var(--brand-2)); }
  .ins-actions { display: flex; gap: 6px; flex-wrap: wrap; margin-top: auto; padding-top: 10px; border-top: 1px solid var(--panel-border); }
  .ins-empty { display: flex; flex-direction: column; gap: 6px; align-items: center; justify-content: center; text-align: center; flex: 1; color: var(--text-dim); padding: 20px 10px; }
  .ins-empty .t { font-weight: 700; }
  .ins-empty .d { font-size: .82rem; }
  .btn.danger { color: var(--danger, #e5484d); }
  .json-area { width: 100%; font-family: var(--font-mono, ui-monospace, monospace); font-size: .78rem; line-height: 1.5; padding: 8px 10px; border-radius: 9px; background: var(--bg); border: 1px solid var(--panel-border); color: inherit; resize: vertical; }
  .json-area.invalid { border-color: var(--danger, #e5484d); }
  .hook-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .hook-url { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .74rem; padding: 6px 9px; background: var(--bg); border: 1px solid var(--panel-border); border-radius: 8px; }

  /* xyflow dark theming to match the design tokens */
  .canvas-shell :global(.svelte-flow) {
    --xy-background-color: transparent;
    --xy-edge-stroke: var(--panel-border);
    --xy-edge-stroke-selected: var(--brand);
    --xy-attribution-background-color: transparent;
  }
  .canvas-shell :global(.svelte-flow__background) { opacity: 0.55; }
  .canvas-shell :global(.svelte-flow__controls) { border-radius: 10px; overflow: hidden; border: 1px solid var(--panel-border); box-shadow: none; }
  .canvas-shell :global(.svelte-flow__controls-button) { background: var(--panel-bg); border-bottom: 1px solid var(--panel-border); color: var(--text-dim); fill: var(--text-dim); }
  .canvas-shell :global(.svelte-flow__controls-button:hover) { background: var(--bg); }
  .canvas-shell :global(.svelte-flow__minimap) { background: var(--panel-bg); border-radius: 10px; border: 1px solid var(--panel-border); overflow: hidden; }
  .canvas-shell :global(.svelte-flow__minimap-mask) { fill: rgb(0 0 0 / 0.45); }
  .canvas-shell :global(.svelte-flow__handle) { width: 8px; height: 8px; background: var(--brand); border: 2px solid var(--bg); }

  @media (max-width: 1100px) {
    .editor-grid { grid-template-columns: 180px 1fr; grid-template-rows: 1fr auto; }
    .inspector { grid-column: 1 / -1; max-height: 320px; }
  }
</style>
