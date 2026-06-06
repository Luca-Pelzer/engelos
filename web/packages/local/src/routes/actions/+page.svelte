<script lang="ts">
  import { api, channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
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

  let channel = $state('');
  let rules = $state<Rule[]>([]);
  let catalog = $state<Catalog>({ conditions: [], actions: [], triggers: [] });
  let loading = $state(false);
  let firing = $state<string | null>(null);

  const triggerOptions: PluginDef[] = [
    { id: 'event', name: 'Event / Nachricht', description: '' },
    { id: 'command', name: 'Command', description: '' },
    { id: 'timer', name: 'Timer / Intervall', description: '' },
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
  let fMode = $state('all');
  let fConds = $state<CondInstance[]>([]);
  let fActs = $state<ActInstance[]>([]);
  let fQueueId = $state('');

  async function load() {
    if (!channel) { return; }
    loading = true;
    try {
      const [list, cat] = await Promise.all([
        channelApi(channel).get<ListResponse>('/actions'),
        api.get<Catalog>('/api/v1/actions/catalog'),
      ]);
      rules = list.rules ?? [];
      catalog = { conditions: cat.conditions ?? [], actions: cat.actions ?? [], triggers: cat.triggers ?? [] };
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'Aktionen-Feature ist nicht aktiviert.' : 'Laden fehlgeschlagen.', 'error');
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
    fMode = 'all';
    fConds = [];
    fActs = catalog.actions.length ? [{ type_id: catalog.actions[0].id, config: {}, enabled: true }] : [];
    fQueueId = '';
  }

  function openNew() {
    if (!catalog.actions.length) { toast('Bitte zuerst laden.', 'warn'); return; }
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
    fMode = r.conditions?.mode || 'all';
    fConds = (r.conditions?.conditions ?? []).map((c) => ({ type_id: c.type_id, config: { ...(c.config ?? {}) } }));
    fActs = (r.actions?.actions ?? []).map((a) => ({ type_id: a.type_id, config: { ...(a.config ?? {}) }, enabled: a.enabled }));
    fQueueId = r.actions?.queue_id ?? '';
    showForm = true;
  }

  function addCond() {
    if (!catalog.conditions.length) return;
    fConds = [...fConds, { type_id: catalog.conditions[0].id, config: {} }];
  }
  function removeCond(i: number) { fConds = fConds.filter((_, idx) => idx !== i); }

  function addAct() {
    if (!catalog.actions.length) return;
    fActs = [...fActs, { type_id: catalog.actions[0].id, config: {}, enabled: true }];
  }
  function removeAct(i: number) { fActs = fActs.filter((_, idx) => idx !== i); }
  function moveAct(i: number, dir: -1 | 1) {
    const j = i + dir;
    if (j < 0 || j >= fActs.length) return;
    const next = [...fActs];
    [next[i], next[j]] = [next[j], next[i]];
    fActs = next;
  }

  function buildTriggerFilter(): Record<string, unknown> | undefined {
    if (fTriggerKind === 'event') return fEventType.trim() ? { event_type: fEventType.trim() } : undefined;
    if (fTriggerKind === 'command') return { command: fCommand.trim() };
    if (fTriggerKind === 'timer') return { interval_seconds: Math.max(5, Math.round(fIntervalMin * 60)) };
    return undefined;
  }

  async function runNow(r: Rule) {
    if (firing) return;
    firing = r.name;
    try {
      await channelApi(channel).post(`/actions/${encodeURIComponent(r.name)}/fire`);
      toast(`"${r.name}" ausgeloest.`, 'success');
    } catch (err) {
      toast(err instanceof ApiException && err.status === 404 ? 'Regel nicht gefunden.' : 'Ausloesen fehlgeschlagen.', 'error');
    } finally {
      firing = null;
    }
  }

  async function save() {
    if (!channel || !fName.trim()) { toast('Name ist erforderlich.', 'warn'); return; }
    if (fActs.length === 0) { toast('Mindestens eine Aktion ist erforderlich.', 'warn'); return; }
    if (fTriggerKind === 'command' && !fCommand.trim()) { toast('Command-Wort ist erforderlich.', 'warn'); return; }

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
        toast('Regel aktualisiert.', 'success');
      } else {
        const r = await channelApi(channel).post<Rule>('/actions', body);
        rules = [...rules, r];
        toast('Regel angelegt.', 'success');
      }
      showForm = false;
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) toast('Name existiert bereits.', 'error');
      else if (err instanceof ApiException && err.status === 400) toast('Ungueltige Regel.', 'error');
      else toast('Speichern fehlgeschlagen.', 'error');
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
      toast('Aenderung fehlgeschlagen.', 'error');
    }
  }

  async function del(name: string) {
    try {
      await channelApi(channel).delete(`/actions/${encodeURIComponent(name)}`);
      rules = rules.filter((x) => x.name !== name);
      toast('Regel geloescht.', 'warn');
    } catch {
      toast('Loeschen fehlgeschlagen.', 'error');
    }
  }

  const condName = (id: string) => catalog.conditions.find((c) => c.id === id)?.name ?? id;
  const actName = (id: string) => catalog.actions.find((a) => a.id === id)?.name ?? id;

  function triggerSummary(r: Rule): string {
    if (r.trigger_kind === 'command') return `Command !${(r.trigger_filter?.command as string)?.replace(/^!/, '') ?? '?'}`;
    if (r.trigger_kind === 'event') {
      const ev = r.trigger_filter?.event_type as string;
      return ev ? `Event ${ev}` : 'Jede Nachricht';
    }
    if (r.trigger_kind === 'timer') {
      const sec = (r.trigger_filter?.interval_seconds as number) ?? 0;
      return sec >= 60 ? `Timer alle ${Math.round(sec / 60)} Min` : `Timer alle ${sec} Sek`;
    }
    if (r.trigger_kind === 'manual') return 'Manuell';
    return r.trigger_kind;
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="actions">
  <div class="page-wrap">
    <div class="toolbar">
      <span class="ws-label">{channel ? `@${channel}` : 'Kein Workspace gewaehlt'}</span>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading || !channel}>{loading ? 'Laedt...' : 'Neu laden'}</button>
      <div class="grow"></div>
      <span class="count-pill"><b>{rules.length}</b> Regeln</span>
      <button class="btn btn-primary btn-sm" onclick={openNew} disabled={!channel}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>Neue Regel</button>
    </div>

    {#if showForm}
      <div class="form-card">
        <div class="form-grid">
          <div><label class="fld" for="action-name">Name</label><div class="input"><input id="action-name" type="text" placeholder="z. B. discord-link" bind:value={fName} disabled={editing !== null} /></div></div>
          <label class="enable-row"><input type="checkbox" bind:checked={fEnabled} /> Aktiviert</label>
        </div>

        <div class="builder-block">
          <div class="block-head"><span class="badge tr">Trigger</span><span class="muted">Was loest die Regel aus?</span></div>
          <div class="form-grid">
            <div>
              <label class="fld" for="action-trigger-kind">Typ</label>
              <div class="input"><select id="action-trigger-kind" bind:value={fTriggerKind}>
                {#each triggerOptions as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
              </select></div>
            </div>
            {#if fTriggerKind === 'event'}
              <div><label class="fld" for="action-event-type">Event-Typ (leer = jede Nachricht)</label><div class="input"><input id="action-event-type" type="text" placeholder="z. B. channel.raided" bind:value={fEventType} /></div></div>
            {:else if fTriggerKind === 'command'}
              <div><label class="fld" for="action-command">Command-Wort</label><div class="input"><input id="action-command" type="text" placeholder="z. B. !discord" bind:value={fCommand} /></div></div>
            {:else if fTriggerKind === 'timer'}
              <div><label class="fld" for="action-interval">Intervall (Minuten)</label><div class="input"><input id="action-interval" type="number" min="1" step="1" bind:value={fIntervalMin} /></div><div class="muted small">Mindestens 5 Sekunden zwischen den Ausloesungen.</div></div>
            {:else if fTriggerKind === 'manual'}
              <div class="muted small">Diese Regel laeuft nur, wenn du sie per "Ausloesen" von Hand startest.</div>
            {/if}
          </div>
        </div>

        <div class="builder-block">
          <div class="block-head">
            <span class="badge co">Bedingungen</span>
            <div class="input" style="max-width:130px"><select bind:value={fMode}>
              <option value="all">Alle (UND)</option>
              <option value="any">Eine (ODER)</option>
              <option value="none">Keine</option>
            </select></div>
            <div class="grow"></div>
            <button class="btn btn-ghost btn-sm" onclick={addCond} disabled={!catalog.conditions.length}>+ Bedingung</button>
          </div>
          {#if fConds.length === 0}<div class="muted small">Keine Bedingungen: die Regel feuert immer.</div>{/if}
          {#each fConds as c, i (i)}
            <div class="row-item">
              <div class="input" style="min-width:180px"><select bind:value={c.type_id}>
                {#each catalog.conditions as cd (cd.id)}<option value={cd.id}>{cd.name}</option>{/each}
              </select></div>
              {#if c.type_id === 'builtin:message-contains'}
                <div class="input grow"><input type="text" placeholder="Text der enthalten sein muss" value={(c.config.substring as string) ?? ''} oninput={(e) => (c.config.substring = e.currentTarget.value)} /></div>
                <label class="enable-row small"><input type="checkbox" checked={(c.config.case_sensitive as boolean) ?? false} onchange={(e) => (c.config.case_sensitive = e.currentTarget.checked)} /> Gross/klein</label>
              {:else if c.type_id === 'builtin:user-role'}
                <div class="input grow"><select value={(c.config.role as string) ?? 'everyone'} onchange={(e) => (c.config.role = e.currentTarget.value)}>
                  <option value="everyone">Jeder</option>
                  <option value="subscriber">Subscriber</option>
                  <option value="vip">VIP</option>
                  <option value="moderator">Moderator</option>
                  <option value="broadcaster">Broadcaster</option>
                </select></div>
              {/if}
              <button class="iact del" onclick={() => removeCond(i)} aria-label="Entfernen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14" /></svg></button>
            </div>
          {/each}
        </div>

        <div class="builder-block">
          <div class="block-head">
            <span class="badge ac">Aktionen</span><span class="muted">Werden der Reihe nach ausgefuehrt</span>
            <div class="grow"></div>
            <button class="btn btn-ghost btn-sm" onclick={addAct} disabled={!catalog.actions.length}>+ Aktion</button>
          </div>
          {#each fActs as a, i (i)}
            <div class="row-item">
              <span class="ord">{i + 1}</span>
              <div class="input" style="min-width:170px"><select bind:value={a.type_id}>
                {#each catalog.actions as ad (ad.id)}<option value={ad.id}>{ad.name}</option>{/each}
              </select></div>
              {#if a.type_id === 'builtin:send-chat'}
                <div class="input grow"><input type="text" placeholder="Nachricht, z. B. Hi $(user)!" value={(a.config.text as string) ?? ''} oninput={(e) => (a.config.text = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'builtin:delay'}
                <div class="input" style="max-width:130px"><input type="number" min="0" step="0.5" placeholder="Sekunden" value={(a.config.seconds as number) ?? 0} oninput={(e) => (a.config.seconds = parseFloat(e.currentTarget.value) || 0)} /></div>
              {:else if a.type_id === 'builtin:log'}
                <div class="input grow"><input type="text" placeholder="Log-Text" value={(a.config.message as string) ?? ''} oninput={(e) => (a.config.message = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'obs:switch-scene'}
                <div class="input grow"><input type="text" placeholder="Szenen-Name, z. B. Starting Soon" value={(a.config.scene as string) ?? ''} oninput={(e) => (a.config.scene = e.currentTarget.value)} /></div>
              {:else if a.type_id === 'obs:set-source-visibility'}
                <div class="input" style="min-width:150px"><input type="text" placeholder="Szene" value={(a.config.scene as string) ?? ''} oninput={(e) => (a.config.scene = e.currentTarget.value)} /></div>
                <div class="input grow"><input type="text" placeholder="Quelle / Source" value={(a.config.source as string) ?? ''} oninput={(e) => (a.config.source = e.currentTarget.value)} /></div>
                <label class="enable-row small"><input type="checkbox" checked={(a.config.visible as boolean) ?? false} onchange={(e) => (a.config.visible = e.currentTarget.checked)} /> Sichtbar</label>
              {/if}
              <div class="ord-btns">
                <button class="iact" onclick={() => moveAct(i, -1)} disabled={i === 0} aria-label="Hoch"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6" /></svg></button>
                <button class="iact" onclick={() => moveAct(i, 1)} disabled={i === fActs.length - 1} aria-label="Runter"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6" /></svg></button>
              </div>
              <button class="iact del" onclick={() => removeAct(i)} aria-label="Entfernen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14" /></svg></button>
            </div>
          {/each}
        </div>

        <div class="form-actions">
          <button class="btn btn-ghost btn-sm" onclick={() => (showForm = false)}>Abbrechen</button>
          <button class="btn btn-primary btn-sm" onclick={save}>Speichern</button>
        </div>
      </div>
    {/if}

    <table class="dtable" style="margin-top:14px">
      <thead><tr><th>Name</th><th>Trigger</th><th class="right" style="width:90px">Aktionen</th><th style="width:90px">Aktiv</th><th class="right" style="width:128px">Aktion</th></tr></thead>
      <tbody>
        {#each rules as r (r.name)}
          <tr>
            <td><b>{r.name}</b></td>
            <td class="muted">{triggerSummary(r)}</td>
            <td class="right num">{r.actions?.actions?.length ?? 0}</td>
            <td><button class="switch" class:on={r.enabled} onclick={() => toggle(r)} aria-label="Umschalten"></button></td>
            <td>
              <div class="row-actions">
                <button class="iact" onclick={() => runNow(r)} disabled={firing === r.name} aria-label="Ausloesen" title="Jetzt ausloesen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3l14 9-14 9z" /></svg></button>
                <button class="iact" onclick={() => openEdit(r)} aria-label="Bearbeiten"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></svg></button>
                <button class="iact del" onclick={() => del(r.name)} aria-label="Loeschen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if rules.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="12" r="2.5"/><path d="M7.5 6H12a4 4 0 0 1 4 4v.5M7.5 18H12a4 4 0 0 0 4-4v-.5"/></svg>
        <div class="t">{channel ? 'Keine Regeln' : 'Workspace oben auswaehlen'}</div>
        <div class="d">Baue Automatisierungen: Trigger, Bedingungen, Aktionen.</div>
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
  .row-item { display: flex; align-items: center; gap: 8px; }
  .ord { width: 22px; height: 22px; flex: 0 0 auto; display: grid; place-items: center; font-size: .78rem; font-weight: 800; color: var(--text-dim); background: var(--panel-bg); border: 1px solid var(--panel-border); border-radius: 7px; }
  .ord-btns { display: flex; gap: 2px; }
  .grow { flex: 1 1 auto; }
  .muted { color: var(--text-dim); }
  .small { font-size: .82rem; }
  .ws-label { font-weight: 700; color: var(--text-dim); font-size: .9rem; }
  @media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } .row-item { flex-wrap: wrap; } }
</style>
