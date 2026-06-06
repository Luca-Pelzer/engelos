<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Status = {
    channel: string;
    viewer_id: string;
    points: number;
    soft_pity_hit: boolean;
    near_guaranteed: boolean;
    effective_chance: number;
    hard_pity_threshold: number;
    soft_pity_fraction: number;
  };

  type RollResult = {
    won: boolean;
    was_guaranteed: boolean;
    points_before: number;
    points_after: number;
    effective_chance: number;
  };

  type Entry = {
    channel: string;
    viewer_id: string;
    username: string;
    points: number;
  };

  type Leaderboard = { channel: string; limit: number; entries: Entry[] };

  const CHANNEL_KEY = 'engelos.pity.channel';

  let channel = $state('');
  let viewerId = $state('');
  let status = $state<Status | null>(null);
  let board = $state<Entry[]>([]);
  let grantAmount = $state(0);
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

  function pct(v: number): string {
    return `${(v * 100).toFixed(1)}%`;
  }

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('Pity system is not enabled on this bot.', 'warn', 6000);
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

  async function loadStatus() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = viewerId.trim();
    if (!vid) {
      toast('Enter a viewer id first.', 'warn');
      return;
    }
    loadingStatus = true;
    try {
      status = await api.get<Status>(
        `/api/v1/pity/status?channel=${encodeURIComponent(ch)}&viewer_id=${encodeURIComponent(vid)}`,
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
        `/api/v1/pity/leaderboard?channel=${encodeURIComponent(ch)}&limit=25`,
      );
      board = res.entries ?? [];
    } catch (err) {
      handleError(err, 'load leaderboard');
    } finally {
      loadingBoard = false;
    }
  }

  async function grant() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = viewerId.trim();
    if (!vid) {
      toast('Enter a viewer id first.', 'warn');
      return;
    }
    busy = true;
    try {
      await api.post('/api/v1/pity/grant', {
        channel: ch,
        viewer_id: vid,
        amount: grantAmount > 0 ? grantAmount : undefined,
        reason: 'dashboard',
      });
      toast('Points granted.', 'success');
      await loadStatus();
      await loadBoard();
    } catch (err) {
      handleError(err, 'grant points');
    } finally {
      busy = false;
    }
  }

  async function roll() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = viewerId.trim();
    if (!vid) {
      toast('Enter a viewer id first.', 'warn');
      return;
    }
    busy = true;
    try {
      const res = await api.post<RollResult>('/api/v1/pity/roll', { channel: ch, viewer_id: vid });
      toast(
        res.won
          ? res.was_guaranteed
            ? 'Win (guaranteed by pity).'
            : 'Win.'
          : 'No win this roll.',
        res.won ? 'success' : 'info',
      );
      await loadStatus();
      await loadBoard();
    } catch (err) {
      handleError(err, 'roll');
    } finally {
      busy = false;
    }
  }

  async function reset() {
    const ch = requireChannel();
    if (!ch) return;
    const vid = viewerId.trim();
    if (!vid) {
      toast('Enter a viewer id first.', 'warn');
      return;
    }
    busy = true;
    try {
      await api.post('/api/v1/pity/reset', { channel: ch, viewer_id: vid, reason: 'dashboard' });
      toast('Pity reset for viewer.', 'success');
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
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Pity</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Inspect and manage the pity loyalty system. Points build up until a win becomes
      near-guaranteed, so engaged viewers always pay off eventually.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Viewer lookup</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-4">
      Enter a channel and a viewer id to inspect or adjust their pity.
    </p>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <Input label="Channel" placeholder="engelswtf" bind:value={channel} />
      <Input label="Viewer id" placeholder="twitch user id" bind:value={viewerId} />
    </div>
    <div class="flex flex-wrap gap-2 mt-4">
      <Button onclick={loadStatus} disabled={loadingStatus}>
        {#snippet children()}{loadingStatus ? 'Loading...' : 'Load status'}{/snippet}
      </Button>
      <Button variant="secondary" onclick={roll} disabled={busy}>
        {#snippet children()}Roll{/snippet}
      </Button>
      <Button variant="secondary" onclick={grant} disabled={busy}>
        {#snippet children()}Grant{/snippet}
      </Button>
      <input
        type="number"
        min="0"
        bind:value={grantAmount}
        placeholder="amount"
        class="grant-amount"
        aria-label="Grant amount"
      />
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
        {#if status.near_guaranteed}
          <Badge tone="warn">Near guaranteed</Badge>
        {:else if status.soft_pity_hit}
          <Badge tone="info">Soft pity</Badge>
        {:else}
          <Badge tone="neutral">Building</Badge>
        {/if}
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div>
          <div class="text-[11.5px] text-fg-soft">Points</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.points}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Win chance</div>
          <div class="text-[18px] font-semibold text-fg-strong">{pct(status.effective_chance)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Hard pity at</div>
          <div class="text-[18px] font-semibold text-fg-strong">{status.hard_pity_threshold}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Soft pity from</div>
          <div class="text-[18px] font-semibold text-fg-strong">{pct(status.soft_pity_fraction)}</div>
        </div>
      </div>
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
        title="No pity data yet"
        description="Once viewers chat and build pity, the top holders show up here."
      />
    {:else}
      <table class="w-full text-[13px]">
        <thead>
          <tr class="text-left text-[11.5px] text-fg-soft">
            <th class="pb-2 font-medium">#</th>
            <th class="pb-2 font-medium">Viewer</th>
            <th class="pb-2 font-medium text-right">Points</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--color-border-soft)]">
          {#each board as e, i (e.viewer_id)}
            <tr>
              <td class="py-2 text-fg-soft">{i + 1}</td>
              <td class="py-2 text-fg">{e.username || e.viewer_id}</td>
              <td class="py-2 text-right font-mono text-accent">{e.points}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Card>
</section>

<style>
  .grant-amount {
    width: 100px;
    padding: 0 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .grant-amount:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
