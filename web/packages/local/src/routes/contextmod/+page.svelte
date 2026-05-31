<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    enabled: boolean;
    rules: string;
    updated_at: string;
  };

  const CHANNEL_KEY = 'engelos.contextmod.channel';
  const MAX_RULES = 4000;

  let channel = $state('');
  let enabled = $state(false);
  let rules = $state('');
  let loading = $state(false);
  let saving = $state(false);
  let loaded = $state(false);

  let remaining = $derived(MAX_RULES - rules.length);

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
        toast('Context moderation is not enabled on this bot.', 'warn', 6000);
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
      const c = await api.get<Config>(`/api/v1/contextmod?channel=${encodeURIComponent(ch)}`);
      enabled = c.enabled;
      rules = c.rules;
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load context-mod settings');
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
    if (rules.length > MAX_RULES) {
      toast(`Rules are too long (max ${MAX_RULES} characters).`, 'warn');
      return;
    }
    saving = true;
    try {
      await api.put<Config>('/api/v1/contextmod', { channel: ch, enabled, rules });
      toast('Context-mod settings saved.', 'success');
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'save context-mod settings');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Context AI Moderation</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      An AI second opinion that reviews messages the rule engine let pass. Describe your channel
      rules in plain language; the AI flags clear violations for deletion or timeout. It never
      acts on mods or the broadcaster and fails open when unsure.
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
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Context moderation</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, passed messages are reviewed against your rules. Leave the rules empty to keep it
        idle even while enabled.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable AI escalation</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <label class="block mt-5">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Channel rules (plain language)</span>
        <textarea
          bind:value={rules}
          rows="6"
          placeholder="No slurs, no threats, no doxxing, no spam. Keep it family-friendly."
          class="contextmod-input resize-y"
        ></textarea>
        <span class="block text-[11.5px] mt-1" class:text-fg-soft={remaining >= 0} class:text-danger={remaining < 0}>
          {remaining} characters left
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
  .contextmod-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
    line-height: 1.5;
  }
  .contextmod-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
