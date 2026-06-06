<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Status = {
    channel: string;
    viewer_id: string;
    days_current: number;
    days_longest: number;
    freezes_available: number;
    last_tick_at: string;
    next_milestone: number;
  };

  type TickResult = {
    days_current: number;
    days_longest: number;
    freezes_available: number;
    milestone: boolean;
    same_day_retick: boolean;
    used_freezes: number;
    broken_from_days: number;
  };

  type Entry = {
    channel: string;
    viewer_id: string;
    username: string;
    days_current: number;
    days_longest: number;
  };

  type Leaderboard = { channel: string; limit: number; entries: Entry[] };

  const CHANNEL_KEY = 'engelos.streak.channel';

  let channel = $state('');
  let viewerId = $state('');
  let status = $state<Status | null>(null);
  let board = $state<Entry[]>([]);
  let loadingStatus = $state(false);
  let loadingBoard = $state(false);
  let busy = $state(false);

  onMount(() => {
    const saved = localStorage.getItem(CHANNEL_KEY);
    if (saved) {
      channel = saved;
      void loadBoard();
    }
  });

  function fmtDate(s: string): string {
    if (!s) return 'never';
    const d = new Date(s);
    return Number.isNaN(d.getTime()) ? s : d.toLocaleString();
  }

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('Streak system is not enabled on this bot.', 'warn', 6000);
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  function requireChannel(): string | null {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel first.', 'warn');
      return null;
    }
    localStorage.setItem(CHANNEL_KEY, ch);
    return ch;
  }

  function requireViewer(): string | null {
    const vid = viewerId.trim();
    if (!vid) {
      toast('Enter a viewer id first.', 'warn');
      return null;
    }
    return vid;
  }

  async function loadStatus() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = requireViewer();
    if (!vid) return;
    loadingStatus = true;
    try {
      status = await api.get<Status>(
        `/api/v1/streak/status?channel=${encodeURIComponent(ch)}&viewer_id=${encodeURIComponent(vid)}`,
      );
    } catch (err) {
      handleError(err, 'load status');
    } finally {
      loadingStatus = false;
    }
  }

  async function loadBoard() {
    const ch = requireChannel();
    if (!ch) return;
    loadingBoard = true;
    try {
      const res = await api.get<Leaderboard>(
        `/api/v1/streak/leaderboard?channel=${encodeURIComponent(ch)}&limit=25`,
      );
      board = res.entries ?? [];
    } catch (err) {
      handleError(err, 'load leaderboard');
    } finally {
      loadingBoard = false;
    }
  }

  async function tick() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = requireViewer();
    if (!vid) return;
    busy = true;
    try {
      const res = await api.post<TickResult>('/api/v1/streak/tick', { channel: ch, viewer_id: vid });
      toast(
        res.same_day_retick
          ? 'Already ticked today.'
          : res.milestone
            ? `Milestone reached at ${res.days_current} days.`
            : `Streak now ${res.days_current} days.`,
        res.milestone ? 'success' : 'info',
      );
      await loadStatus();
      await loadBoard();
    } catch (err) {
      handleError(err, 'tick');
    } finally {
      busy = false;
    }
  }

  async function freeze() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = requireViewer();
    if (!vid) return;
    busy = true;
    try {
      const res = await api.post<TickResult>('/api/v1/streak/freeze', { channel: ch, viewer_id: vid });
      toast(`Freeze used. ${res.freezes_available} left.`, 'success');
      await loadStatus();
      await loadBoard();
    } catch (err) {
      handleError(err, 'use freeze');
    } finally {
      busy = false;
    }
  }

  async function reset() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = requireViewer();
    if (!vid) return;
    busy = true;
    try {
      await api.post('/api/v1/streak/reset', { channel: ch, viewer_id: vid, reason: 'dashboard' });
      toast('Streak reset for viewer.', 'success');
      await loadStatus();
      await loadBoard();
    } catch (err) {
      handleError(err, 'reset');
    } finally {
      busy = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Streak</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Track daily viewer streaks. Viewers keep momentum by showing up each day, and freezes
      let them protect a streak when they miss a day.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Viewer lookup</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-4">
      Enter a channel and viewer id to inspect or adjust a streak.
    </p>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <Input label="Channel" placeholder="engelswtf" bind:value={channel} />
      <Input label="Viewer id" placeholder="twitch user id" bind:value={viewerId} />
    </div>
    <div class="flex flex-wrap gap-2 mt-4">
      <Button onclick={loadStatus} disabled={loadingStatus}>
        {#snippet children()}{loadingStatus ? 'Loading...' : 'Load status'}{/snippet}
      </Button>
      <Button variant="secondary" onclick={tick} disabled={busy}>
        {#snippet children()}Tick{/snippet}
      </Button>
      <Button variant="secondary" onclick={freeze} disabled={busy}>
        {#snippet children()}Use freeze{/snippet}
      </Button>
      <Button variant="danger" onclick={reset} disabled={busy}>
        {#snippet children()}Reset{/snippet}
      </Button>
    </div>
  </Card>

  {#if status}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-3">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">
          Status for <span class="font-mono text-accent">{status.viewer_id}</span>
        </h3>
        <Badge tone={status.days_current > 0 ? 'accent' : 'neutral'}>
          {status.days_current} day streak
        </Badge>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div>
          <div class="text-[11.5px] text-fg-soft">Current</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.days_current}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Longest</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.days_longest}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Freezes</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.freezes_available}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Next milestone</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.next_milestone}</div>
        </div>
      </div>
      <div class="text-[12px] text-fg-soft mt-3">Last tick: {fmtDate(status.last_tick_at)}</div>
    </Card>
  {/if}

  <Card class="reveal-up reveal-up-delay-3">
    <div class="flex items-center justify-between mb-3">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg">Leaderboard</h3>
      <Button variant="ghost" size="sm" onclick={loadBoard} disabled={loadingBoard}>
        {#snippet children()}{loadingBoard ? 'Loading...' : 'Refresh'}{/snippet}
      </Button>
    </div>
    {#if board.length === 0}
      <EmptyState
        title="No streaks yet"
        description="Once viewers start showing up daily, the longest streaks appear here."
      />
    {:else}
      <table class="w-full text-[13px]">
        <thead>
          <tr class="text-left text-[11.5px] text-fg-soft">
            <th class="pb-2 font-medium">#</th>
            <th class="pb-2 font-medium">Viewer</th>
            <th class="pb-2 font-medium text-right">Current</th>
            <th class="pb-2 font-medium text-right">Longest</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--color-border-soft)]">
          {#each board as e, i (e.viewer_id)}
            <tr>
              <td class="py-2 text-fg-soft">{i + 1}</td>
              <td class="py-2 text-fg">{e.username || e.viewer_id}</td>
              <td class="py-2 text-right font-mono text-accent">{e.days_current}</td>
              <td class="py-2 text-right font-mono text-fg-soft">{e.days_longest}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Card>
</section>
