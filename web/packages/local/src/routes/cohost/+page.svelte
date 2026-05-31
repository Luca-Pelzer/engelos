<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    enabled: boolean;
    bot_name: string;
    persona: string;
    max_reply_len: number;
    updated_at: string;
  };

  const CHANNEL_KEY = 'engelos.cohost.channel';

  let channel = $state('');
  let enabled = $state(false);
  let botName = $state('');
  let persona = $state('');
  let maxReplyLen = $state(280);
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

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('Co-Host is not enabled on this bot.', 'warn', 6000);
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel first.', 'warn');
      return;
    }
    loading = true;
    try {
      const c = await api.get<Config>(`/api/v1/cohost?channel=${encodeURIComponent(ch)}`);
      enabled = c.enabled;
      botName = c.bot_name;
      persona = c.persona;
      maxReplyLen = c.max_reply_len;
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load co-host settings');
    } finally {
      loading = false;
    }
  }

  async function save() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel first.', 'warn');
      return;
    }
    saving = true;
    try {
      await api.put<Config>('/api/v1/cohost', {
        channel: ch,
        enabled,
        bot_name: botName,
        persona,
        max_reply_len: maxReplyLen,
      });
      toast('Co-Host settings saved.', 'success');
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'save co-host settings');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">AI Co-Host</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      An AI sidekick that answers when viewers address it by name. Give it a persona and a
      reply length, then enable it per channel.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Channel</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-4">Pick the channel to configure.</p>
    <div class="flex gap-2 items-end">
      <div class="flex-1">
        <Input label="Channel login" placeholder="engelswtf" bind:value={channel} />
      </div>
      <Button variant="ghost" onclick={load} disabled={loading}>
        {#snippet children()}{loading ? 'Loading...' : 'Load'}{/snippet}
      </Button>
    </div>
  </Card>

  {#if loaded}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-1">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Co-Host</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, the co-host replies to messages that address it by name.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable co-host</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-5">
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Bot name (how viewers address it)</span>
          <input bind:value={botName} placeholder="engel" class="cohost-input" />
        </label>
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Max reply length (characters)</span>
          <input type="number" min="0" max="500" bind:value={maxReplyLen} class="cohost-input" />
        </label>
      </div>

      <label class="block mt-4">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Persona</span>
        <textarea
          bind:value={persona}
          rows="3"
          placeholder="a friendly, concise stream co-host"
          class="cohost-input resize-y"
        ></textarea>
        <span class="block text-[11.5px] text-fg-soft mt-1">
          A short style instruction. Keep it brief; it shapes how the co-host talks.
        </span>
      </label>
    </Card>

    <div class="flex justify-end gap-2 pt-2 reveal-up reveal-up-delay-3">
      <Button onclick={save} disabled={saving}>
        {#snippet children()}{saving ? 'Saving...' : 'Save changes'}{/snippet}
      </Button>
    </div>
  {/if}
</section>

<style>
  .cohost-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .cohost-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
