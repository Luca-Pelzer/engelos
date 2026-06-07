<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Card, Button, Badge } from '@engelos/shared/components';
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';

  type Moment = {
    id: string;
    channel: string;
    title: string;
    status: string;
    rarity: string;
    participants: number;
    opened_by: string;
    opened_at: string;
    closes_at: string;
    closed_at?: string;
  };

  type GetResponse = { active: Moment | null; history: Moment[] };

  let channel = $state('');
  let active = $state<Moment | null>(null);
  let history = $state<Moment[]>([]);
  let title = $state('');
  let windowSec = $state(60);
  let loading = $state(false);
  let opening = $state(false);
  let ending = $state(false);
  let loaded = $state(false);
  let poll: ReturnType<typeof setInterval> | null = null;

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => {
    if (channel) void load();
    poll = setInterval(() => { if (channel) void load(true); }, 5000);
  });
  onDestroy(() => { if (poll) clearInterval(poll); });

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('Moments are not enabled on this bot.', 'warn', 6000);
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  async function load(silent = false) {
    if (!channel) { return; }
    if (!silent) { loading = true; }
    try {
      const res = await channelApi(channel).get<GetResponse>('/moments?limit=25');
      active = res.active;
      history = res.history ?? [];
      loaded = true;
    } catch (err) {
      if (!silent) { handleError(err, 'load moments'); }
    } finally {
      if (!silent) { loading = false; }
    }
  }

  async function open() {
    if (!channel || title.trim() === '') { return; }
    opening = true;
    try {
      active = await channelApi(channel).post<Moment>('/moments', {
        title: title.trim(),
        window_sec: windowSec,
      });
      title = '';
      toast('Moment started.', 'success');
      void load(true);
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) {
        toast('A moment is already active.', 'warn');
      } else {
        handleError(err, 'start moment');
      }
    } finally {
      opening = false;
    }
  }

  async function end() {
    if (!channel) { return; }
    ending = true;
    try {
      const closed = await channelApi(channel).post<Moment>('/moments/end', {});
      active = null;
      toast(`Moment ended: ${closed.rarity} (${closed.participants} reacted).`, 'success', 6000);
      void load(true);
    } catch (err) {
      handleError(err, 'end moment');
    } finally {
      ending = false;
    }
  }

  function rarityTone(rarity: string): 'neutral' | 'info' | 'accent' {
    if (rarity === 'legendary') { return 'accent'; }
    if (rarity === 'rare') { return 'info'; }
    return 'neutral';
  }

  function fmtTime(iso: string): string {
    if (!iso) { return ''; }
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleString();
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Moments</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      BeReal-style moment alerts. Start a moment when something big happens; viewers type
      <code>!here</code> in chat to react before the window closes. The more who react, the rarer it gets.
    </p>
  </header>

  {#if !channel}
    <Card class="reveal-up reveal-up-delay-1">
      <p class="text-[13px] text-fg-soft">Choose a workspace above to manage moments.</p>
    </Card>
  {/if}

  {#if loaded}
    {#if active}
      <Card class="reveal-up reveal-up-delay-2">
        <div class="flex items-start justify-between mb-1">
          <h3 class="text-[14px] font-semibold tracking-tight text-fg">Live moment</h3>
          <Badge tone="success">Open</Badge>
        </div>
        <p class="text-[15px] text-fg-strong mt-2">{active.title}</p>
        <div class="flex items-center gap-4 mt-3 text-[13px] text-fg-soft">
          <span><strong class="text-fg-strong">{active.participants}</strong> reacted</span>
          <span>closes {fmtTime(active.closes_at)}</span>
        </div>
        <div class="flex justify-end mt-5">
          <Button onclick={end} disabled={ending}>
            {#snippet children()}{ending ? 'Ending...' : 'End moment now'}{/snippet}
          </Button>
        </div>
      </Card>
    {:else}
      <Card class="reveal-up reveal-up-delay-2">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Start a moment</h3>
        <p class="text-[12.5px] text-fg-soft mb-5">
          Fires a <code>moment.opened</code> overlay alert and opens the <code>!here</code> window.
        </p>
        <div class="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-4">
          <label class="block">
            <span class="block text-[12.5px] text-fg-soft mb-1.5">Title</span>
            <input bind:value={title} placeholder="Insane clutch!" class="moment-input" />
          </label>
          <label class="block">
            <span class="block text-[12.5px] text-fg-soft mb-1.5">Window (seconds)</span>
            <input type="number" min="10" max="600" bind:value={windowSec} class="moment-input w-32" />
          </label>
        </div>
        <div class="flex justify-end mt-5">
          <Button onclick={open} disabled={opening || title.trim() === ''}>
            {#snippet children()}{opening ? 'Starting...' : 'Start moment'}{/snippet}
          </Button>
        </div>
      </Card>
    {/if}

    <Card class="reveal-up reveal-up-delay-3">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-4">Archive</h3>
      {#if history.length === 0}
        <p class="text-[13px] text-fg-soft">No moments yet. Start one above.</p>
      {:else}
        <div class="space-y-2">
          {#each history as m (m.id)}
            <div class="flex items-center justify-between py-2 border-b border-soft last:border-0">
              <div class="min-w-0">
                <span class="text-[13px] text-fg truncate">{m.title}</span>
                <span class="block text-[11.5px] text-fg-soft">{fmtTime(m.closed_at ?? m.opened_at)}</span>
              </div>
              <div class="flex items-center gap-3 shrink-0">
                <span class="text-[12.5px] text-fg-soft">{m.participants} reacted</span>
                <Badge tone={rarityTone(m.rarity)}>{m.rarity}</Badge>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </Card>
  {/if}
</section>

<style>
  .moment-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .moment-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
