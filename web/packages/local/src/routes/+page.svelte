<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Card, Badge, StatusDot, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, wsStatus, botStatus, events } from '@engelos/shared/lib';

  type Dispatcher = {
    messages: number;
    subscriptions: number;
    raids: number;
    pity_grant_errors: number;
    streak_tick_errors: number;
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
      { label: 'Messages', value: d ? fmt(d.messages) : '—' },
      { label: 'Subscriptions', value: d ? fmt(d.subscriptions) : '—' },
      { label: 'Raids', value: d ? fmt(d.raids) : '—' },
      {
        label: 'Handler Errors',
        value: d ? fmt(d.pity_grant_errors + d.streak_tick_errors) : '—',
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
  <header class="flex items-end justify-between gap-4 reveal-up">
    <div>
      <p class="text-[13px] text-fg-soft mb-1">Welcome back</p>
      <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">
        Your stream's pulse, at a glance.
      </h2>
    </div>
    <div class="flex items-center gap-2 text-[12.5px] text-fg-soft whitespace-nowrap">
      <StatusDot state={dotState} />
      {$botStatus.label}
    </div>
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
        <div class="flex flex-col gap-1">
          <span class="text-[12px] uppercase tracking-wider text-muted font-medium">{c.label}</span>
          <span class="text-[28px] leading-none font-semibold tracking-tight text-fg-strong font-mono mt-1">
            {loading ? '…' : c.value}
          </span>
          <span class="text-[12.5px] mt-2 text-fg-soft">since last restart</span>
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

    <Card class="reveal-up reveal-up-delay-5">
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
</style>
