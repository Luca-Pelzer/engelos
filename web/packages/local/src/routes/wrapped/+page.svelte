<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type TopChatter = { username: string; messages: number };

  type ChannelCard = {
    kind: 'channel';
    channel: string;
    period: string;
    total_messages: number;
    total_subs: number;
    total_raids: number;
    total_viewers: number;
    top_chatters: TopChatter[];
  };

  type ViewerCard = {
    kind: 'viewer';
    channel: string;
    period: string;
    viewer: string;
    username: string;
    messages: number;
    subs: number;
    sub_gifts: number;
    rank: number;
    percentile: number;
    points?: number;
    longest_streak?: number;
  };

  type WrappedCard = ChannelCard | ViewerCard;

  const CHANNEL_KEY = 'engelos.wrapped.channel';

  let channel = $state('');
  let viewer = $state('');
  let period = $state('all');
  let card = $state<WrappedCard | null>(null);
  let loading = $state(false);

  onMount(() => {
    const saved = localStorage.getItem(CHANNEL_KEY);
    if (saved) {
      channel = saved;
      void load();
    }
  });

  function fmt(n: number): string {
    return n.toLocaleString();
  }

  function handleError(err: unknown) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 404) {
        toast('No wrapped data for that viewer or period yet.', 'warn', 6000);
      } else if (err.status === 501) {
        toast('Wrapped is not enabled on this bot.', 'warn', 6000);
      } else if (err.status === 400) {
        toast(err.message || 'Invalid channel or period.', 'error', 6000);
      } else {
        toast(err.message || 'Could not load wrapped.', 'error', 6000);
      }
    } else {
      toast('Could not load wrapped.', 'error', 6000);
    }
  }

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel first.', 'warn');
      return;
    }
    localStorage.setItem(CHANNEL_KEY, ch);
    loading = true;
    card = null;
    try {
      const params = new URLSearchParams({ channel: ch });
      const v = viewer.trim();
      if (v) params.set('viewer', v);
      const p = period.trim();
      if (p && p !== 'all') params.set('period', p);
      card = await api.get<WrappedCard>(`/api/v1/wrapped?${params.toString()}`);
    } catch (err) {
      handleError(err);
    } finally {
      loading = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Stream Wrapped</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Year-in-review recap cards for the channel or a single viewer. Leave the viewer field
      empty for the channel summary, or set a period like 2026-05 for a single month.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Build a card</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-4">
      Channel plus optional viewer id. Period is all-time or YYYY-MM.
    </p>
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
      <Input label="Channel" placeholder="engelswtf" bind:value={channel} />
      <Input label="Viewer id (optional)" placeholder="twitch user id" bind:value={viewer} />
      <label class="block">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Period</span>
        <input bind:value={period} placeholder="all or 2026-05" class="wrapped-input" />
      </label>
    </div>
    <div class="flex justify-end mt-4">
      <Button onclick={load} disabled={loading}>
        {#snippet children()}{loading ? 'Loading...' : 'Generate card'}{/snippet}
      </Button>
    </div>
  </Card>

  {#if card && card.kind === 'channel'}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-4">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">
          {card.channel} <span class="text-fg-soft">channel recap</span>
        </h3>
        <Badge tone="accent">{card.period}</Badge>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div>
          <div class="text-[11.5px] text-fg-soft">Messages</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.total_messages)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Subs</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.total_subs)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Raids</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.total_raids)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Viewers</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.total_viewers)}</div>
        </div>
      </div>

      <h4 class="text-[12.5px] font-semibold text-fg mt-6 mb-2">Top chatters</h4>
      {#if card.top_chatters.length === 0}
        <p class="text-[12.5px] text-fg-soft">No chat activity in this period yet.</p>
      {:else}
        <table class="w-full text-[13px]">
          <tbody class="divide-y divide-[var(--color-border-soft)]">
            {#each card.top_chatters as c, i (c.username + i)}
              <tr>
                <td class="py-2 text-fg-soft w-8">{i + 1}</td>
                <td class="py-2 text-fg">{c.username}</td>
                <td class="py-2 text-right font-mono text-accent">{fmt(c.messages)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </Card>
  {:else if card && card.kind === 'viewer'}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-4">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">
          {card.username || card.viewer} <span class="text-fg-soft">in {card.channel}</span>
        </h3>
        <div class="flex gap-2">
          {#if card.rank > 0}
            <Badge tone="accent">Top {card.percentile}%</Badge>
          {/if}
          <Badge tone="neutral">{card.period}</Badge>
        </div>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div>
          <div class="text-[11.5px] text-fg-soft">Messages</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.messages)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Rank</div>
          <div class="text-[20px] font-semibold text-fg-strong">{card.rank > 0 ? `#${card.rank}` : 'n/a'}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Subs</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.subs)}</div>
        </div>
        <div>
          <div class="text-[11.5px] text-fg-soft">Gift subs</div>
          <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.sub_gifts)}</div>
        </div>
        {#if card.points !== undefined}
          <div>
            <div class="text-[11.5px] text-fg-soft">Loyalty points</div>
            <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.points)}</div>
          </div>
        {/if}
        {#if card.longest_streak !== undefined}
          <div>
            <div class="text-[11.5px] text-fg-soft">Longest streak</div>
            <div class="text-[20px] font-semibold text-fg-strong">{fmt(card.longest_streak)} days</div>
          </div>
        {/if}
      </div>
    </Card>
  {:else if !loading}
    <Card class="reveal-up reveal-up-delay-2" padded={false}>
      <EmptyState
        title="No card yet"
        description="Enter a channel and generate a recap. Add a viewer id for a personal card."
      />
    </Card>
  {/if}
</section>

<style>
  .wrapped-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .wrapped-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
