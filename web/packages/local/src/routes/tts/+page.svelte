<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge } from '@engelos/shared/components';
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    enabled: boolean;
    voice_id: string;
    model: string;
    has_api_key: boolean;
    updated_at: string;
  };

  type Voice = { voice_id: string; name: string; category: string };

  let channel = $state('');
  let enabled = $state(false);
  let voiceId = $state('');
  let model = $state('eleven_flash_v2_5');
  let hasApiKey = $state(false);
  let apiKey = $state('');
  let voices = $state<Voice[]>([]);
  let loading = $state(false);
  let saving = $state(false);
  let loadingVoices = $state(false);
  let loaded = $state(false);

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('AI Voice is not enabled on this bot.', 'warn', 6000);
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  async function load() {
    if (!channel) { return; }
    loading = true;
    try {
      const c = await channelApi(channel).get<Config>('/tts');
      enabled = c.enabled;
      voiceId = c.voice_id;
      model = c.model || 'eleven_flash_v2_5';
      hasApiKey = c.has_api_key;
      loaded = true;
    } catch (err) {
      handleError(err, 'load voice settings');
    } finally {
      loading = false;
    }
  }

  async function save() {
    if (!channel) { return; }
    saving = true;
    try {
      const body: Record<string, unknown> = { enabled, voice_id: voiceId, model };
      if (apiKey.trim() !== '') { body.api_key = apiKey.trim(); }
      const c = await channelApi(channel).put<Config>('/tts', body);
      hasApiKey = c.has_api_key;
      apiKey = '';
      toast('Voice settings saved.', 'success');
    } catch (err) {
      handleError(err, 'save voice settings');
    } finally {
      saving = false;
    }
  }

  async function loadVoices() {
    if (!channel) { return; }
    loadingVoices = true;
    try {
      const res = await channelApi(channel).get<{ voices: Voice[] }>('/tts/voices');
      voices = res.voices ?? [];
      if (voices.length === 0) { toast('No voices found on this account.', 'warn'); }
    } catch (err) {
      handleError(err, 'load voices');
    } finally {
      loadingVoices = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">AI Voice</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Read alerts aloud with an ElevenLabs voice. Add your own ElevenLabs API key, pick a
      voice, and add the audio overlay to OBS as a browser source.
    </p>
  </header>

  {#if !channel}
    <Card class="reveal-up reveal-up-delay-1">
      <p class="text-[13px] text-fg-soft">Choose a workspace above to configure AI Voice.</p>
    </Card>
  {/if}

  {#if loaded}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-1">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Voice alerts</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, new subscriptions are spoken aloud through the audio overlay.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable voice alerts</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <label class="block mt-5">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">
          ElevenLabs API key {hasApiKey ? '(a key is saved; leave blank to keep it)' : '(required)'}
        </span>
        <input
          type="password"
          bind:value={apiKey}
          placeholder={hasApiKey ? '••••••••••••' : 'sk-...'}
          autocomplete="off"
          class="tts-input"
        />
      </label>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-4">
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Voice</span>
          {#if voices.length > 0}
            <select bind:value={voiceId} class="tts-input">
              <option value="">Select a voice...</option>
              {#each voices as v (v.voice_id)}
                <option value={v.voice_id}>{v.name} ({v.category})</option>
              {/each}
            </select>
          {:else}
            <input bind:value={voiceId} placeholder="voice id" class="tts-input" />
          {/if}
          <button type="button" class="tts-link" onclick={loadVoices} disabled={loadingVoices}>
            {loadingVoices ? 'Loading voices...' : 'Load voices from my account'}
          </button>
        </label>
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Model</span>
          <select bind:value={model} class="tts-input">
            <option value="eleven_flash_v2_5">Flash v2.5 (fastest)</option>
            <option value="eleven_multilingual_v2">Multilingual v2 (highest quality)</option>
          </select>
        </label>
      </div>

      <div class="mt-5 p-3 rounded-md bg-[var(--color-surface)] border border-soft">
        <span class="block text-[12.5px] text-fg-soft mb-1">OBS browser source</span>
        <code class="text-[12px] text-fg">/overlay/tts</code>
        <span class="block text-[11.5px] text-fg-soft mt-1">
          Add this as a browser source in OBS. It plays the spoken alerts; the key stays on the bot.
        </span>
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
  .tts-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .tts-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .tts-link {
    margin-top: 6px;
    font-size: 11.5px;
    color: var(--color-accent);
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
  }
  .tts-link:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
