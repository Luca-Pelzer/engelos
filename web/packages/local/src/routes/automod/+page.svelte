<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge, StatusDot } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import AiModNav from '$lib/AiModNav.svelte';

  // Config mirrors automod.Config exactly. Go marshals with field names (no
  // json tags), so the keys here are PascalCase to match the wire format.
  type Config = {
    Mode: number; // 0=off, 1=dry-run, 2=active
    Caps: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; MinLength: number; MaxCapsPercent: number };
    Symbols: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; MaxGroupedSymbols: number; MaxSymbolPercent: number; MinLengthForPercent: number; BlockZalgo: boolean };
    Links: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; AllowList: string[] | null; BlockIPAddresses: boolean; BlockDotVariants: boolean };
    Emotes: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; MaxEmotes: number };
    Length: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; MaxChars: number };
    Repetition: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; MinLength: number; MaxRepeatRatio: number };
    BannedWords: { Enabled: boolean; ExemptMinRole: number; TimeoutSecs: number; Entries: unknown[] | null };
  };

  type AuditAction = {
    id: string;
    username: string;
    message_text: string;
    filter_name: string;
    reason: string;
    action: string;
    duration_sec: number;
    dry_run: boolean;
    created_at: string;
    // AI-escalation fields, present only on AI rows (omitted for fast-path rows).
    ai_category?: string;
    ai_severity?: number;
    ai_confidence?: number;
    ai_consulted?: boolean;
  };

  // Engine mode rendered as explicit cards so the safe choice is obvious and
  // the enforcing choice is clearly labelled. Values map to automod.FilterMode
  // (0=off, 1=dry-run/shadow, 2=active).
  const MODE_CARDS = [
    { value: 0, label: 'Off', hint: 'The engine is disabled. No messages are inspected.' },
    { value: 1, label: 'Dry-run', hint: 'Shadow mode. Every rule is evaluated and logged, but nobody is timed out or banned. Start here and watch the audit log.' },
    { value: 2, label: 'Active', hint: 'Real Twitch actions (delete / timeout / ban) are carried out. Switch on only after Dry-run looks right.' },
  ];
  const ROLES = ['Everyone', 'Subscribers', 'VIPs', 'Moderators', 'Broadcaster'];
  const AUDIT_CHANNEL_KEY = 'engelos.automod.channel';

  let cfg = $state<Config | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let loadError = $state<string | null>(null);

  let auditChannel = $state('');
  let audit = $state<AuditAction[]>([]);
  let auditLoaded = $state(false);
  let auditLoading = $state(false);
  let sourceFilter = $state('all'); // 'all' | 'fast' | 'ai'

  // The structural filters that share the (enable, role, timeout) shape, plus
  // their one or two numeric knobs, rendered generically.
  const FILTERS: { key: keyof Config; label: string; desc: string }[] = [
    { key: 'Caps', label: 'Caps', desc: 'Too much SHOUTING (uppercase ratio).' },
    { key: 'Links', label: 'Links', desc: 'URLs not on the allow-list.' },
    { key: 'Symbols', label: 'Symbols', desc: 'Symbol spam and zalgo text.' },
    { key: 'Emotes', label: 'Emotes', desc: 'Too many emotes in one message.' },
    { key: 'Length', label: 'Length', desc: 'Walls of text.' },
    { key: 'Repetition', label: 'Repetition', desc: 'The same word spammed over and over.' },
    { key: 'BannedWords', label: 'Banned words', desc: 'Phrases you never want in chat.' },
  ];

  onMount(() => {
    void load();
    const saved = localStorage.getItem(AUDIT_CHANNEL_KEY);
    if (saved) {
      auditChannel = saved;
      void loadAudit();
    }
  });

  async function load() {
    loading = true;
    try {
      cfg = await api.get<Config>('/api/v1/automod/config');
      loadError = null;
    } catch (err) {
      loadError = err instanceof ApiException && err.status === 401 ? 'Sign in to manage AutoMod.' : 'Failed to load AutoMod config.';
    } finally {
      loading = false;
    }
  }

  async function save() {
    if (!cfg) return;
    saving = true;
    try {
      cfg = await api.put<Config>('/api/v1/automod/config', cfg);
      toast('AutoMod settings saved.', 'success');
    } catch (err) {
      if (err instanceof ApiException) {
        toast(err.message || 'Failed to save.', 'error', 6000);
      } else {
        toast('Failed to save.', 'error', 6000);
      }
    } finally {
      saving = false;
    }
  }

  async function loadAudit() {
    const ch = auditChannel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel first.', 'warn');
      return;
    }
    auditLoading = true;
    try {
      let url = `/api/v1/automod/audit?channel=${encodeURIComponent(ch)}&limit=100`;
      if (sourceFilter !== 'all') url += `&source=${sourceFilter}`;
      const res = await api.get<{ actions: AuditAction[] }>(url);
      audit = res.actions ?? [];
      auditLoaded = true;
      localStorage.setItem(AUDIT_CHANNEL_KEY, ch);
    } catch (err) {
      const msg = err instanceof ApiException && err.status === 401 ? 'Sign in to view the audit log.' : 'Failed to load audit log.';
      toast(msg, 'error', 6000);
    } finally {
      auditLoading = false;
    }
  }

  // Generic numeric-field helpers keep the per-filter markup terse while still
  // writing back to the exact PascalCase keys the Go config expects.
  function num(section: Record<string, unknown>, field: string): number {
    return (section[field] as number) ?? 0;
  }
  function setNum(section: Record<string, unknown>, field: string, value: string) {
    section[field] = Number(value);
    cfg = cfg; // trigger reactivity
  }

  function actionTone(a: string): 'neutral' | 'warn' | 'danger' | 'accent' {
    if (a === 'ban') return 'danger';
    if (a === 'timeout') return 'warn';
    if (a === 'delete') return 'accent';
    return 'neutral';
  }

  // An AI-escalation row carries the verdict fields (ai_consulted is present);
  // fast-path rows omit them entirely.
  function isAIRow(a: AuditAction): boolean {
    return a.ai_consulted !== undefined;
  }

  // AI severity 0-3, tinted from calm to alarming.
  function severityTone(sev: number): 'success' | 'info' | 'warn' | 'danger' {
    if (sev >= 3) return 'danger';
    if (sev === 2) return 'warn';
    if (sev === 1) return 'info';
    return 'success';
  }

  function fmtDuration(sec: number): string {
    if (sec <= 0) return '—';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    if (sec < 86400) return `${Math.round(sec / 3600)}h`;
    return `${Math.round(sec / 86400)}d`;
  }
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">AI-Mod</p>
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Fast Path · AutoMod Rules</h2>
    <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
      The deterministic first layer of moderation. These rules run on every
      message and act in milliseconds — before the AI escalation step. Moderators
      and the broadcaster are always exempt. In active mode, repeat offenders
      escalate: warn → 1m → 10m → 24h → ban.
    </p>
  </header>

  <AiModNav />

  <header class="flex items-end justify-between gap-4 reveal-up reveal-up-delay-1">
    <div>
      <p class="text-[12.5px] text-muted">Step 2 of the pipeline</p>
    </div>
    {#if cfg}
      <Button onclick={save} loading={saving}>
        {#snippet children()}Save changes{/snippet}
      </Button>
    {/if}
  </header>

  {#if loading}
    <Card class="reveal-up"><p class="text-[13px] text-fg-soft">Loading…</p></Card>
  {:else if loadError}
    <Card class="reveal-up">
      <div class="flex items-center gap-2.5">
        <StatusDot state="warn" pulse={false} />
        <span class="text-[13px] text-fg-soft">{loadError}</span>
      </div>
    </Card>
  {:else if cfg}
    <Card class="reveal-up reveal-up-delay-1">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Engine mode</h3>
      <p class="text-[12.5px] text-fg-soft mb-3.5">
        Dry-run is the safe place to start: it records what the rules would do
        without acting on anyone. Move to Active only after the shadow log looks right.
      </p>
      <div class="mode-grid">
        {#each MODE_CARDS as m (m.value)}
          <label class="mode-card" class:selected={cfg.Mode === m.value}>
            <input
              type="radio"
              name="engine-mode"
              value={m.value}
              checked={cfg.Mode === m.value}
              onchange={() => { if (cfg) cfg.Mode = m.value; }}
            />
            <span class="mode-card-head">
              <span class="mode-card-label">{m.label}</span>
              {#if m.value === 1}<Badge tone="success">Recommended</Badge>{:else if m.value === 2}<Badge tone="warn">Enforces</Badge>{/if}
            </span>
            <span class="mode-card-hint">{m.hint}</span>
          </label>
        {/each}
      </div>
    </Card>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {#each FILTERS as f (f.key)}
        {@const section = cfg[f.key] as unknown as Record<string, unknown>}
        <Card class="reveal-up reveal-up-delay-2">
          <div class="flex items-start justify-between gap-3">
            <div>
              <div class="flex items-center gap-2">
                <h3 class="text-[14px] font-semibold tracking-tight text-fg">{f.label}</h3>
                {#if section.Enabled}<Badge tone="success">On</Badge>{:else}<Badge tone="neutral">Off</Badge>{/if}
              </div>
              <p class="text-[12.5px] text-fg-soft mt-0.5">{f.desc}</p>
            </div>
            <label class="fp-switch shrink-0">
              <input type="checkbox" bind:checked={section.Enabled as boolean} onchange={() => (cfg = cfg)} />
              <span class="slider"></span>
            </label>
          </div>

          {#if section.Enabled}
            <div class="mt-4 grid grid-cols-2 gap-3">
              <label class="block">
                <span class="block text-[12px] text-fg-soft mb-1">Exempt role ≥</span>
                <select class="select" bind:value={section.ExemptMinRole} onchange={() => (cfg = cfg)}>
                  {#each ROLES as role, i (i)}<option value={i}>{role}</option>{/each}
                </select>
              </label>
              <label class="block">
                <span class="block text-[12px] text-fg-soft mb-1">Timeout (sec)</span>
                <input class="field" type="number" value={num(section, 'TimeoutSecs')} oninput={(e) => setNum(section, 'TimeoutSecs', e.currentTarget.value)} />
              </label>

              {#if f.key === 'Caps'}
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Min length</span><input class="field" type="number" value={num(section, 'MinLength')} oninput={(e) => setNum(section, 'MinLength', e.currentTarget.value)} /></label>
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Max caps ratio</span><input class="field" type="number" step="0.05" value={num(section, 'MaxCapsPercent')} oninput={(e) => setNum(section, 'MaxCapsPercent', e.currentTarget.value)} /></label>
              {:else if f.key === 'Emotes'}
                <label class="block col-span-2"><span class="block text-[12px] text-fg-soft mb-1">Max emotes</span><input class="field" type="number" value={num(section, 'MaxEmotes')} oninput={(e) => setNum(section, 'MaxEmotes', e.currentTarget.value)} /></label>
              {:else if f.key === 'Length'}
                <label class="block col-span-2"><span class="block text-[12px] text-fg-soft mb-1">Max characters</span><input class="field" type="number" value={num(section, 'MaxChars')} oninput={(e) => setNum(section, 'MaxChars', e.currentTarget.value)} /></label>
              {:else if f.key === 'Repetition'}
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Min length</span><input class="field" type="number" value={num(section, 'MinLength')} oninput={(e) => setNum(section, 'MinLength', e.currentTarget.value)} /></label>
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Max repeat ratio</span><input class="field" type="number" step="0.05" value={num(section, 'MaxRepeatRatio')} oninput={(e) => setNum(section, 'MaxRepeatRatio', e.currentTarget.value)} /></label>
              {:else if f.key === 'Symbols'}
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Max symbol run</span><input class="field" type="number" value={num(section, 'MaxGroupedSymbols')} oninput={(e) => setNum(section, 'MaxGroupedSymbols', e.currentTarget.value)} /></label>
                <label class="block"><span class="block text-[12px] text-fg-soft mb-1">Max symbol ratio</span><input class="field" type="number" step="0.05" value={num(section, 'MaxSymbolPercent')} oninput={(e) => setNum(section, 'MaxSymbolPercent', e.currentTarget.value)} /></label>
              {/if}
            </div>
          {/if}
        </Card>
      {/each}
    </div>

    <Card padded={false} class="reveal-up">
      <div class="flex items-center justify-between px-5 py-4 border-b border-soft">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Audit log</h3>
        <div class="flex items-end gap-2">
          <select class="select max-w-[130px]" aria-label="Source filter" bind:value={sourceFilter} onchange={() => { if (auditLoaded) void loadAudit(); }}>
            <option value="all">All</option>
            <option value="fast">Fast path</option>
            <option value="ai">AI</option>
          </select>
          <form class="flex items-end gap-2" onsubmit={(e) => { e.preventDefault(); void loadAudit(); }}>
            <input class="field max-w-[180px]" placeholder="channel login" bind:value={auditChannel} />
            <Button type="submit" variant="secondary" size="sm" loading={auditLoading}>
              {#snippet children()}Load{/snippet}
            </Button>
          </form>
        </div>
      </div>
      {#if !auditLoaded}
        <p class="px-5 py-6 text-[13px] text-muted">Enter a channel to view recent moderation actions.</p>
      {:else if audit.length === 0}
        <div class="px-5 py-6">
          <p class="text-[13px] text-fg-soft">No decisions recorded yet.</p>
          <p class="text-[12.5px] text-muted mt-1 max-w-xl">
            AutoMod writes a row here for every decision — including Dry-run previews —
            once chat events are processed for this channel. An empty log before live
            traffic is expected, not a problem.
          </p>
        </div>
      {:else}
        <table class="w-full text-left">
          <thead>
            <tr class="text-[11px] uppercase tracking-wider text-muted">
              <th class="px-5 py-3 font-medium">When</th>
              <th class="px-5 py-3 font-medium">User</th>
              <th class="px-5 py-3 font-medium">Filter</th>
              <th class="px-5 py-3 font-medium">Source</th>
              <th class="px-5 py-3 font-medium">Action</th>
              <th class="px-5 py-3 font-medium">Reason</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[var(--color-border-soft)]">
            {#each audit as a (a.id)}
              <tr class="hover:bg-[var(--color-bg-soft)] transition-colors">
                <td class="px-5 py-3 text-[12px] text-muted whitespace-nowrap tabular-nums">{a.created_at.replace('T', ' ').slice(0, 19)}</td>
                <td class="px-5 py-3 text-[13px] text-fg">{a.username}</td>
                <td class="px-5 py-3 text-[13px] text-fg-soft">{a.filter_name}</td>
                <td class="px-5 py-3">
                  {#if isAIRow(a)}
                    <div class="flex flex-wrap items-center gap-1.5">
                      <Badge tone="info">AI</Badge>
                      {#if a.ai_category && a.ai_category !== 'none'}<Badge tone="neutral">{a.ai_category}</Badge>{/if}
                      {#if a.ai_severity !== undefined}<Badge tone={severityTone(a.ai_severity)}>sev {a.ai_severity}</Badge>{/if}
                      {#if a.ai_confidence !== undefined}<span class="text-[11px] text-muted tabular-nums">{Math.round(a.ai_confidence * 100)}%</span>{/if}
                    </div>
                  {:else}
                    <Badge tone="neutral">Fast path</Badge>
                  {/if}
                </td>
                <td class="px-5 py-3">
                  <Badge tone={actionTone(a.action)}>{a.action}{a.duration_sec > 0 ? ' ' + fmtDuration(a.duration_sec) : ''}</Badge>
                  {#if a.dry_run}<span class="ml-1.5 text-[11px] text-muted">(dry-run)</span>{/if}
                </td>
                <td class="px-5 py-3 text-[12.5px] text-fg-soft max-w-[260px] truncate">{a.reason}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </Card>
  {/if}
</section>

<style>
  .select, .field {
    display: block;
    width: 100%;
    height: 38px;
    padding: 0 11px;
    border-radius: var(--radius-md);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
    outline: none;
    transition: border-color 150ms, background 150ms;
  }
  .select:focus, .field:focus {
    border-color: var(--color-accent);
    background: var(--color-surface);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
  .fp-switch { position: relative; display: inline-block; width: 40px; height: 22px; }
  .fp-switch input { opacity: 0; width: 0; height: 0; }
  .slider {
    position: absolute; inset: 0; cursor: pointer;
    background: var(--color-border); border-radius: 999px; transition: background 150ms;
  }
  .slider::before {
    content: ''; position: absolute; height: 16px; width: 16px; left: 3px; bottom: 3px;
    background: #fff; border-radius: 50%; transition: transform 150ms;
  }
  .fp-switch input:checked + .slider { background: var(--color-accent); }
  .fp-switch input:checked + .slider::before { transform: translateX(18px); }
  .mode-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 8px;
  }
  @media (min-width: 720px) {
    .mode-grid { grid-template-columns: repeat(3, 1fr); }
  }
  .mode-card {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 12px 13px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-bg-soft);
    cursor: pointer;
    transition: border-color 150ms, background 150ms, box-shadow 150ms;
  }
  .mode-card:hover { border-color: var(--color-accent); }
  .mode-card.selected {
    border-color: var(--color-accent);
    background: var(--color-surface);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
  .mode-card input {
    position: absolute;
    opacity: 0;
    pointer-events: none;
  }
  .mode-card-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .mode-card-label {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--color-fg);
  }
  .mode-card-hint {
    font-size: 12px;
    color: var(--color-fg-soft);
    line-height: 1.5;
  }
</style>
