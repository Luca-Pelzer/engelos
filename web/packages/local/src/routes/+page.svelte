<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Card, Badge, StatusDot, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, wsStatus, events } from '@engelos/shared/lib';

  type Dispatcher = {
    messages: number;
    subscriptions: number;
    raids: number;
    dropped_events: number;
    last_event_at: string;
  };
  type StatsResponse = { version: string; phase: string; dispatcher?: Dispatcher };

  let stats = $state<StatsResponse | null>(null);
  let statsError = $state<string | null>(null);
  let loading = $state(true);
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  const dotState = $derived(
    $wsStatus === 'open' ? 'online' : $wsStatus === 'connecting' ? 'connecting' : 'offline',
  );

  const cards = $derived.by(() => {
    const d = stats?.dispatcher;
    return [
      {
        label: 'Messages',
        value: d ? fmt(d.messages) : '—',
        warn: false,
        icon: '<path d="M4 5.5h16v10H9.5l-4 3v-3H4z"/>',
      },
      {
        label: 'Subscriptions',
        value: d ? fmt(d.subscriptions) : '—',
        warn: false,
        icon: '<path d="M12 3.5l2.5 5.2 5.7.7-4.2 4 1.1 5.6-5.1-2.8-5.1 2.8 1.1-5.6-4.2-4 5.7-.7z"/>',
      },
      {
        label: 'Raids',
        value: d ? fmt(d.raids) : '—',
        warn: false,
        icon: '<path d="M5 19L19 5M19 5h-8M19 5v8"/>',
      },
      {
        label: 'Dropped Events',
        value: d ? fmt(d.dropped_events) : '—',
        warn: !!d && d.dropped_events > 0,
        icon: '<path d="M12 3.5L21.5 20h-19z"/><path d="M12 10v4M12 17h.01"/>',
      },
    ];
  });

  const lastEvent = $derived.by(() => {
    const ts = stats?.dispatcher?.last_event_at;
    if (!ts) return null;
    const t = new Date(ts).getTime();
    if (Number.isNaN(t) || t <= 0) return null;
    return relTime(t);
  });

  async function loadStats() {
    try {
      stats = await api.get<StatsResponse>('/api/v1/stats');
      statsError = null;
    } catch (err) {
      statsError =
        err instanceof ApiException && err.status === 0
          ? 'Daemon unreachable'
          : err instanceof ApiException
            ? err.message
            : 'Failed to load stats';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void loadStats();
    pollTimer = setInterval(loadStats, 10_000);
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
  });

  const shortcuts = [
    {
      href: '/ai-mod', title: 'AI-Mod', color: 'var(--brand)',
      desc: 'Two-layer moderation: instant rules plus an AI second opinion on your plain-language channel rules.',
      cta: 'Tune moderation',
      icon: '<path d="M12 3l8 4v5c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V7z"/><path d="M9.5 12l1.8 1.8L15 10"/>',
    },
    {
      href: '/actions/canvas', title: 'Workflow Canvas', color: 'var(--brand-2)',
      desc: 'Build automations visually: triggers, conditions and 20+ actions from chat to OBS, TTS and AI.',
      cta: 'Open the canvas',
      icon: '<circle cx="5" cy="6" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="12" r="2.5"/><path d="M7.5 6H12a4 4 0 0 1 4 4v.5M7.5 18H12a4 4 0 0 0 4-4v-.5"/>',
    },
    {
      href: '/kb', title: 'Knowledge Base', color: 'var(--twitch)',
      desc: 'Teach the bot your rules, schedule and lore so it answers viewer questions with your facts.',
      cta: 'Add knowledge',
      icon: '<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>',
    },
    {
      href: '/integrations', title: 'Integrations', color: 'var(--spotify)',
      desc: 'Connect ElevenLabs, OBS, Ko-fi, Discord and more — every integration adds workflow nodes.',
      cta: 'Connect services',
      icon: '<path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z"/><path d="M12 16v5"/>',
    },
  ];

  function fmt(n: number): string {
    return n.toLocaleString('en-US');
  }

  function relTime(ms: number): string {
    const diff = Date.now() - ms;
    if (diff < 0) return 'just now';
    const s = Math.floor(diff / 1000);
    if (s < 60) return `${s}s ago`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    return `${Math.floor(h / 24)}d ago`;
  }
</script>

<section class="space-y-7">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">Welcome back</p>
    <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">
      Your control room at a glance.
    </h2>
  </header>

  {#if statsError}
    <Card class="reveal-up reveal-up-delay-1">
      <div class="flex items-center gap-2.5">
        <StatusDot state="warn" pulse={false} />
        <span class="text-[13px] text-fg-soft">
          {statsError} — showing what we have. The dashboard retries every 10s.
        </span>
      </div>
    </Card>
  {/if}

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
    {#each cards as c, i (c.label)}
      <Card class="reveal-up reveal-up-delay-{i + 1}">
        <div class="kpi" class:warn={c.warn}>
          <div class="kpi-head">
            <span class="kpi-ic" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                {@html c.icon}
              </svg>
            </span>
            <span class="kpi-label">{c.label}</span>
          </div>
          <span class="kpi-value">{loading ? '…' : c.value}</span>
          <span class="kpi-caption">since last restart</span>
        </div>
      </Card>
    {/each}
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-5">
    <Card class="lg:col-span-2 reveal-up reveal-up-delay-5" padded={false}>
      <div class="flex items-center justify-between px-5 py-4 border-b border-soft">
        <div class="flex items-center gap-2">
          <h3 class="text-[14px] font-semibold tracking-tight text-fg">Recent Activity</h3>
          {#if $wsStatus === 'open'}
            <Badge tone="accent">Live</Badge>
          {:else}
            <Badge tone="neutral">Offline</Badge>
          {/if}
        </div>
        <a href="/chat" class="text-[12.5px] text-fg-soft hover:text-accent transition-colors">
          View all →
        </a>
      </div>
      {#if $events.length === 0}
        <EmptyState
          title="No events yet"
          description="Once you go live, follows, subs and bot actions stream in here in real time."
        />
      {:else}
        <ul class="divide-y divide-[var(--color-border-soft)]">
          {#each $events.slice(-8).reverse() as ev, i (i)}
            <li class="flex items-center gap-3.5 px-5 py-3.5">
              <span class="activity-dot"></span>
              <div class="flex-1 min-w-0">
                <span class="font-mono text-[13px] text-fg-strong">{ev.type}</span>
              </div>
              {#if ev.ts}
                <time class="text-[12px] text-muted tabular-nums">{relTime(ev.ts)}</time>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </Card>

    <Card class="reveal-up reveal-up-delay-5 self-start">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Daemon</h3>
      <p class="text-[12.5px] text-fg-soft mb-4">Live instance details.</p>
      <dl class="space-y-3 text-[13px]">
        <div class="flex items-center justify-between">
          <dt class="text-fg-soft">Version</dt>
          <dd class="font-mono text-fg">{stats?.version ?? '—'}</dd>
        </div>
        <div class="flex items-center justify-between">
          <dt class="text-fg-soft">Phase</dt>
          <dd class="font-mono text-fg">{stats?.phase ?? '—'}</dd>
        </div>
        <div class="flex items-center justify-between">
          <dt class="text-fg-soft">Connection</dt>
          <dd class="flex items-center gap-2">
            <StatusDot state={dotState} pulse={false} />
            <span class="font-mono text-fg">{$wsStatus}</span>
          </dd>
        </div>
        <div class="flex items-center justify-between">
          <dt class="text-fg-soft">Last event</dt>
          <dd class="font-mono text-fg">{lastEvent ?? 'never'}</dd>
        </div>
      </dl>
    </Card>
  </div>

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
    {#each shortcuts as s, i (s.href)}
      <a href={s.href} class="reveal-up reveal-up-delay-{i + 2}" data-sveltekit-preload-data="hover">
        <Card interactive>
          <div class="flex items-center gap-3 mb-2.5">
            <span class="qa-ic" style="--qa:{s.color}" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">{@html s.icon}</svg>
            </span>
            <h3 class="text-[14px] font-semibold tracking-tight text-fg">{s.title}</h3>
          </div>
          <p class="text-[12.5px] text-fg-soft leading-relaxed">{s.desc}</p>
          <span class="text-[12px] text-accent mt-3 inline-block">{s.cta} →</span>
        </Card>
      </a>
    {/each}
  </div>
</section>

<style>
  .activity-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 18%, transparent);
    flex-shrink: 0;
  }
  .kpi {
    display: flex;
    flex-direction: column;
  }
  .kpi-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
  }
  .kpi-ic {
    width: 26px;
    height: 26px;
    border-radius: var(--radius-sm);
    background: var(--color-accent-soft);
    color: var(--color-accent);
    display: grid;
    place-items: center;
    flex: none;
  }
  .kpi-ic svg {
    width: 14px;
    height: 14px;
  }
  .kpi-label {
    font-size: 11.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-muted);
  }
  .kpi-value {
    font-family: var(--font-mono);
    font-size: 30px;
    line-height: 1;
    font-weight: 600;
    letter-spacing: -0.02em;
    color: var(--color-fg-strong);
    font-variant-numeric: tabular-nums;
  }
  .kpi-caption {
    font-size: 12px;
    color: var(--color-muted);
    margin-top: 8px;
  }
  .kpi.warn .kpi-ic {
    background: color-mix(in srgb, var(--color-warn) 14%, transparent);
    color: var(--color-warn);
  }
  .kpi.warn .kpi-value {
    color: var(--color-warn);
  }
  .qa-ic {
    width: 34px;
    height: 34px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--qa) 16%, transparent);
    color: var(--qa);
    display: grid;
    place-items: center;
    flex: none;
  }
  .qa-ic svg {
    width: 17px;
    height: 17px;
  }
</style>
