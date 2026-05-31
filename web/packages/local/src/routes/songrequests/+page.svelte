<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, Badge } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Config = {
    channel: string;
    provider: string;
    spotify_playlist_id: string;
    max_duration_sec: number;
    enabled: boolean;
    updated_at: string;
  };

  const CHANNEL_KEY = 'engelos.songrequests.channel';

  let channel = $state('');
  let provider = $state('');
  let spotifyPlaylistId = $state('');
  let maxDurationSec = $state(0);
  let enabled = $state(true);
  let loading = $state(false);
  let saving = $state(false);
  let loaded = $state(false);

  let isSpotify = $derived(provider === 'spotify');

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
        toast('Song requests are not enabled on this bot.', 'warn', 6000);
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
      const c = await api.get<Config>(`/api/v1/songrequests?channel=${encodeURIComponent(ch)}`);
      provider = c.provider;
      spotifyPlaylistId = c.spotify_playlist_id;
      maxDurationSec = c.max_duration_sec;
      enabled = c.enabled;
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load song-request settings');
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
    if (maxDurationSec < 0) {
      toast('Max duration cannot be negative.', 'warn');
      return;
    }
    saving = true;
    try {
      await api.put<Config>('/api/v1/songrequests', {
        channel: ch,
        provider,
        spotify_playlist_id: isSpotify ? spotifyPlaylistId : '',
        max_duration_sec: maxDurationSec,
        enabled,
      });
      toast('Song-request settings saved.', 'success');
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'save song-request settings');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Song Requests</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Let viewers queue tracks from chat. Pick a provider, cap the track length, and point
      Spotify at the playlist requests are added to.
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
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Song requests</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'On' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        When on, viewers can queue tracks with the song-request command.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable song requests</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-5">
        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Provider</span>
          <select bind:value={provider} class="songreq-input">
            <option value="">None (disabled)</option>
            <option value="youtube">YouTube</option>
            <option value="spotify">Spotify</option>
          </select>
        </label>

        <label class="block">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Max track length (seconds, 0 = no limit)</span>
          <input type="number" min="0" bind:value={maxDurationSec} class="songreq-input" />
        </label>
      </div>

      {#if isSpotify}
        <label class="block mt-4">
          <span class="block text-[12.5px] text-fg-soft mb-1.5">Spotify playlist id</span>
          <input
            bind:value={spotifyPlaylistId}
            placeholder="37i9dQZF1DXcBWIGoYBM5M"
            class="songreq-input"
          />
          <span class="block text-[11.5px] text-fg-soft mt-1">
            Requests are added to this playlist. Find the id in the Spotify playlist share link.
          </span>
        </label>
      {/if}
    </Card>

    <div class="flex justify-end gap-2 pt-2 reveal-up reveal-up-delay-3">
      <Button onclick={save} disabled={saving}>
        {#snippet children()}{saving ? 'Saving...' : 'Save changes'}{/snippet}
      </Button>
    </div>
  {/if}
</section>

<style>
  .songreq-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
  }
  .songreq-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
</style>
