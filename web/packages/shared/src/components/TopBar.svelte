<script lang="ts">
  import StatusDot from './StatusDot.svelte';
  import { botStatus, wsStatus } from '../lib/stores';
  import { theme, setTheme, accent, setAccent, ACCENTS } from '../lib/theme';

  type Props = { title?: string; path?: string };
  let { title, path }: Props = $props();
</script>

<header class="appbar">
  <div class="crumb">
    {#if path}<span class="path">{path}</span>{/if}
    {#if title}<h1>{title}</h1>{/if}
  </div>

  <div class="appbar-right">
    <span class="status-pill">
      <StatusDot
        state={$wsStatus === 'open' ? 'online' : $wsStatus === 'connecting' ? 'connecting' : 'offline'}
      />
      <span class="status-label">{$botStatus.label}</span>
    </span>

    <div class="accentpick" role="group" aria-label="Akzentfarbe">
      {#each ACCENTS as a (a.id)}
        <button
          type="button"
          class:on={$accent.id === a.id}
          style="background:linear-gradient(135deg,{a.v[0]},{a.v[1]})"
          title={a.name}
          aria-label={a.name}
          onclick={() => setAccent(a)}
        ></button>
      {/each}
    </div>

    <div class="toggle" role="group" aria-label="Theme">
      <button type="button" class:on={$theme === 'dark'} onclick={() => setTheme('dark')} aria-label="Dunkel">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" /></svg>
      </button>
      <button type="button" class:on={$theme === 'light'} onclick={() => setTheme('light')} aria-label="Hell">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>
      </button>
    </div>
  </div>
</header>

<style>
  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 32px;
    padding: 0 11px;
    border-radius: 999px;
    background: var(--panel-2);
    border: 1px solid var(--border);
  }
  .status-label {
    font-size: 12.5px;
    color: var(--text-dim);
    font-weight: 600;
  }
</style>
