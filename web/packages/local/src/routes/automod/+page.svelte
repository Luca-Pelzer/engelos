<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge, StatusDot } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

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
  };

  const MODES = ['Off', 'Dry-run (shadow)', 'Active'];
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
      const res = await api.get<{ actions: AuditAction[] }>(
        `/api/v1/automod/audit?channel=${encodeURIComponent(ch)}&limit=100`,
      );
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

  function fmtDuration(sec: number): string {
    if (sec <= 0) return '—';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    if (sec < 86400) return `${Math.round(sec / 3600)}h`;
    return `${Math.round(sec / 86400)}d`;
  }
</script>

<section class="space-y-6">
  <header class="flex items-end justify-between gap-4 reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">AutoMod</h2>
      <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
        Automatic chat moderation. Moderators and the broadcaster are always
        exempt. Repeat offenders escalate: warn → 1m → 10m → 24h → ban.
      </p>
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
      <label class="block">
        <span class="block text-[13px] font-medium text-fg-soft mb-1.5">Engine mode</span>
        <select class="select max-w-xs" bind:value={cfg.Mode}>
          {#each MODES as m, i (i)}
            <option value={i}>{m}</option>
          {/each}
        </select>
        <span class="block text-[12px] text-muted mt-1.5">
          Dry-run logs what would happen without timing anyone out — perfect for tuning.
        </span>
      </label>
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
            <label class="switch shrink-0">
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
        <form class="flex items-end gap-2" onsubmit={(e) => { e.preventDefault(); void loadAudit(); }}>
          <input class="field max-w-[180px]" placeholder="channel login" bind:value={auditChannel} />
          <Button type="submit" variant="secondary" size="sm" loading={auditLoading}>
            {#snippet children()}Load{/snippet}
          </Button>
        </form>
      </div>
      {#if !auditLoaded}
        <p class="px-5 py-6 text-[13px] text-muted">Enter a channel to view recent moderation actions.</p>
      {:else if audit.length === 0}
        <p class="px-5 py-6 text-[13px] text-muted">No moderation actions recorded yet.</p>
      {:else}
        <table class="w-full text-left">
          <thead>
            <tr class="text-[11px] uppercase tracking-wider text-muted">
              <th class="px-5 py-3 font-medium">When</th>
              <th class="px-5 py-3 font-medium">User</th>
              <th class="px-5 py-3 font-medium">Filter</th>
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
  .switch { position: relative; display: inline-block; width: 40px; height: 22px; }
  .switch input { opacity: 0; width: 0; height: 0; }
  .slider {
    position: absolute; inset: 0; cursor: pointer;
    background: var(--color-border); border-radius: 999px; transition: background 150ms;
  }
  .slider::before {
    content: ''; position: absolute; height: 16px; width: 16px; left: 3px; bottom: 3px;
    background: #fff; border-radius: 50%; transition: transform 150ms;
  }
  .switch input:checked + .slider { background: var(--color-accent); }
  .switch input:checked + .slider::before { transform: translateX(18px); }
</style>
