<script lang="ts">
  import StatusDot from './StatusDot.svelte';
  import { botStatus, wsStatus } from '../lib/stores';

  type Props = { title?: string };
  let { title }: Props = $props();
</script>

<header class="topbar">
  <div class="flex items-center gap-3 min-w-0">
    {#if title}
      <h1 class="text-[15px] font-semibold tracking-tight text-[var(--color-fg)] truncate">
        {title}
      </h1>
    {/if}
  </div>

  <div class="flex items-center gap-3">
    <div class="status-pill">
      <StatusDot
        state={$wsStatus === 'open' ? 'online' : $wsStatus === 'connecting' ? 'connecting' : 'offline'}
      />
      <span class="text-[12.5px] text-[var(--color-fg-soft)] font-medium">
        {$botStatus.label}
      </span>
    </div>

    <button class="user-button" type="button" aria-label="Account menu">
      <span class="avatar">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="9" r="3.2" />
          <path d="M5.5 19c1.2-3.2 3.7-5 6.5-5s5.3 1.8 6.5 5" />
        </svg>
      </span>
    </button>
  </div>
</header>

<style>
  .topbar {
    height: 56px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 24px;
    border-bottom: 1px solid var(--color-border-soft);
    background: color-mix(in srgb, var(--color-bg) 80%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    position: sticky;
    top: 0;
    z-index: 20;
  }
  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 30px;
    padding: 0 11px;
    border-radius: 999px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
  }
  .user-button {
    width: 32px;
    height: 32px;
    border-radius: 999px;
    background: var(--color-surface-2);
    border: 1px solid var(--color-border);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--color-fg-soft);
    transition: border-color var(--duration-fast), color var(--duration-fast);
  }
  .user-button:hover {
    border-color: var(--color-accent);
    color: var(--color-fg);
  }
  .avatar svg {
    width: 18px;
    height: 18px;
  }
</style>
