<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge } from '@engelos/shared/components';
  import { api, toast } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    enabled: boolean;
    keyword_threshold: number;
    emote_threshold: number;
    copypasta_threshold: number;
    min_messages: number;
    spike_factor: number;
    composite_threshold: number;
    cooldown_seconds: number;
    updated_at: string;
  };

  const CHANNEL_KEY = 'engelos.clipper.channel';

  let channel = $state('');
  let enabled = $state(false);
  let keywordThreshold = $state(0);
  let emoteThreshold = $state(0);
  let copypastaThreshold = $state(0);
  let minMessages = $state(0);
  let spikeFactor = $state(0);
  let compositeThreshold = $state(0);
  let cooldownSeconds = $state(0);
  let loading = $state(false);
  let saving = $state(false);
  let loaded = $state(false);

  onMount(() => {
    const saved = localStorage.getItem(CHANNEL_KEY);
    if (saved) {
      channel = saved;
      void load();
    }
  });

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel login first.', 'warn');
      return;
    }
    loading = true;
    try {
      const c = await api.get<Config>(`/api/v1/clipper?channel=${encodeURIComponent(ch)}`);
      enabled = c.enabled;
      keywordThreshold = c.keyword_threshold || 0;
      emoteThreshold = c.emote_threshold || 0;
      copypastaThreshold = c.copypasta_threshold || 0;
      minMessages = c.min_messages || 0;
      spikeFactor = c.spike_factor || 0;
      compositeThreshold = c.composite_threshold || 0;
      cooldownSeconds = c.cooldown_seconds || 0;
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch {
      toast('Could not load clipper settings.', 'error');
    } finally {
      loading = false;
    }
  }

  async function save() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel login first.', 'warn');
      return;
    }
    saving = true;
    try {
      await api.put<Config>('/api/v1/clipper', {
        channel: ch,
        enabled,
        keyword_threshold: keywordThreshold,
        emote_threshold: emoteThreshold,
        copypasta_threshold: copypastaThreshold,
        min_messages: minMessages,
        spike_factor: spikeFactor,
        composite_threshold: compositeThreshold,
        cooldown_seconds: cooldownSeconds,
      });
      toast('Clipper settings saved.', 'success');
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch {
      toast('Could not save clipper settings.', 'error');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Auto-Clipper</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Detect hype moments and clip them automatically. Set 0 on any field to keep the smart default.
      Lower the thresholds for a small channel where few viewers coincide.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Channel</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-4">Pick the channel to configure.</p>
    <div class="flex gap-2 items-end">
      <label class="flex-1 block">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Channel login</span>
        <input bind:value={channel} placeholder="yourchannel" class="form-select" />
      </label>
      <Button variant="ghost" onclick={load} disabled={loading}>
        {#snippet children()}{loading ? 'Loading...' : 'Load'}{/snippet}
      </Button>
    </div>
  </Card>

  {#if loaded}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-1">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Auto-Clipper</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, a clip is captured once enough distinct viewers signal hype within a few seconds.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable auto-clipping</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-5">
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Clip-keyword viewers</span>
          <input type="number" min="0" bind:value={keywordThreshold} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Hype-emote viewers</span>
          <input type="number" min="0" bind:value={emoteThreshold} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Copypasta viewers</span>
          <input type="number" min="0" bind:value={copypastaThreshold} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Min messages in window</span>
          <input type="number" min="0" bind:value={minMessages} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Rate spike factor</span>
          <input type="number" min="0" step="0.1" bind:value={spikeFactor} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Composite threshold (0 to 1)</span>
          <input type="number" min="0" max="1" step="0.05" bind:value={compositeThreshold} class="form-select" />
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Cooldown (seconds)</span>
          <input type="number" min="0" bind:value={cooldownSeconds} class="form-select" />
        </label>
      </div>
    </Card>

    <div class="flex justify-end gap-2 pt-2 reveal-up reveal-up-delay-3">
      <Button onclick={save} disabled={saving}>
        {#snippet children()}{saving ? 'Saving...' : 'Save changes'}{/snippet}
      </Button>
    </div>
  {/if}
</section>

<style>
  .form-select {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .form-select:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
