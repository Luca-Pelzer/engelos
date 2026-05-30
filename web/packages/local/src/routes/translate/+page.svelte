<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge } from '@engelos/shared/components';
  import { api, toast } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    enabled: boolean;
    target_lang: string;
    output_mode: string;
    min_word_count: number;
    updated_at: string;
  };

  const LANGS: { value: string; label: string }[] = [
    { value: 'en', label: 'English' },
    { value: 'es', label: 'Spanish' },
    { value: 'de', label: 'German' },
    { value: 'fr', label: 'French' },
    { value: 'pt', label: 'Portuguese' },
    { value: 'it', label: 'Italian' },
    { value: 'nl', label: 'Dutch' },
    { value: 'pl', label: 'Polish' },
    { value: 'ru', label: 'Russian' },
    { value: 'ja', label: 'Japanese' },
    { value: 'ko', label: 'Korean' },
    { value: 'zh', label: 'Chinese' },
  ];

  const OUTPUT_MODES: { value: string; label: string }[] = [
    { value: 'chat', label: 'Post to chat' },
    { value: 'reply', label: 'Reply to author' },
  ];

  const CHANNEL_KEY = 'engelos.translate.channel';

  let channel = $state('');
  let enabled = $state(false);
  let targetLang = $state('en');
  let outputMode = $state('chat');
  let minWords = $state(2);
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
      const c = await api.get<Config>(`/api/v1/translate?channel=${encodeURIComponent(ch)}`);
      enabled = c.enabled;
      targetLang = c.target_lang || 'en';
      outputMode = c.output_mode || 'chat';
      minWords = c.min_word_count || 2;
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch {
      toast('Could not load translation settings.', 'error');
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
      await api.put<Config>('/api/v1/translate', {
        channel: ch,
        enabled,
        target_lang: targetLang,
        output_mode: outputMode,
        min_word_count: minWords,
      });
      toast('Translation settings saved.', 'success');
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch {
      toast('Could not save translation settings.', 'error');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Translation</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Auto-translate chat messages with AI. Off by default; it uses your Claude subscription.
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
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Translation</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, messages not already in the target language are translated and posted back.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable translation</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-5">
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Target language</span>
          <select bind:value={targetLang} class="form-select">
            {#each LANGS as l (l.value)}
              <option value={l.value}>{l.label}</option>
            {/each}
          </select>
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Output mode</span>
          <select bind:value={outputMode} class="form-select">
            {#each OUTPUT_MODES as m (m.value)}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
        </label>
      </div>

      <label class="block mt-4 max-w-[12rem]">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Min words to translate</span>
        <input type="number" min="0" bind:value={minWords} class="form-select" />
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
