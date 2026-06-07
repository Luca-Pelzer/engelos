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

  let cloneName = $state('');
  let cloneFiles = $state<FileList | null>(null);
  let cloning = $state(false);
  let deletingVoice = $state(false);

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

  async function createClone() {
    if (!channel || cloneName.trim() === '' || !cloneFiles || cloneFiles.length === 0) { return; }
    cloning = true;
    try {
      const fd = new FormData();
      fd.append('name', cloneName.trim());
      for (const file of Array.from(cloneFiles)) { fd.append('files', file); }
      const res = await channelApi(channel).post<{ voice_id: string; requires_verification: boolean }>('/tts/clone', fd);
      await loadVoices();
      voiceId = res.voice_id;
      cloneName = '';
      cloneFiles = null;
      if (res.requires_verification) {
        toast('Voice created, but ElevenLabs needs verification before it can speak. Verify it in your ElevenLabs dashboard.', 'warn', 7000);
      } else {
        toast('Voice clone created and selected.', 'success');
      }
    } catch (err) {
      handleError(err, 'create voice clone');
    } finally {
      cloning = false;
    }
  }

  async function deleteSelectedVoice() {
    if (!channel || !voiceId) { return; }
    deletingVoice = true;
    try {
      await channelApi(channel).delete('/tts/voices/' + encodeURIComponent(voiceId));
      voiceId = '';
      await loadVoices();
      toast('Voice deleted.', 'success');
    } catch (err) {
      handleError(err, 'delete voice');
    } finally {
      deletingVoice = false;
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
          {#if voiceId}
            <button type="button" class="tts-link tts-link-danger" onclick={deleteSelectedVoice} disabled={deletingVoice}>
              {deletingVoice ? 'Deleting...' : 'Delete this voice'}
            </button>
          {/if}
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

    <Card class="reveal-up reveal-up-delay-3">
      <div class="flex items-start justify-between mb-1">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Clone your own voice</h3>
        <Badge tone="neutral">ElevenLabs</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        Upload audio to train a voice clone, then pick it as the voice above. Works best with one to
        two minutes of clear speech with no background noise.
      </p>

      {#if !hasApiKey}
        <p class="text-[12.5px] text-fg-soft mb-4">Save your ElevenLabs API key above first.</p>
      {/if}

      <label class="block">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Voice name</span>
        <input bind:value={cloneName} placeholder="My voice" class="tts-input" />
      </label>

      <label class="block mt-4">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Audio samples</span>
        <input
          type="file"
          accept="audio/*"
          multiple
          onchange={(e) => { cloneFiles = e.currentTarget.files; }}
          class="tts-input tts-file"
        />
        {#if cloneFiles && cloneFiles.length > 0}
          <span class="block text-[11.5px] text-fg-soft mt-1.5">{cloneFiles.length} file{cloneFiles.length === 1 ? '' : 's'} selected</span>
        {/if}
      </label>

      <div class="flex justify-end mt-5">
        <Button onclick={createClone} disabled={cloning || !hasApiKey || cloneName.trim() === '' || !cloneFiles || cloneFiles.length === 0}>
          {#snippet children()}{cloning ? 'Creating...' : 'Create voice clone'}{/snippet}
        </Button>
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
  .tts-link-danger {
    margin-left: 12px;
    color: var(--color-danger, #ef4444);
  }
  .tts-file {
    padding: 7px 11px;
    cursor: pointer;
  }
  .tts-file::file-selector-button {
    margin-right: 10px;
    padding: 4px 10px;
    border-radius: var(--radius-sm, 6px);
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-fg);
    font-size: 12px;
    cursor: pointer;
  }
</style>
