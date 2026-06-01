<script lang="ts">
  import { theme, toggleTheme } from '../lib/theme';

  let isLight = $derived($theme === 'light');
</script>

<button
  type="button"
  class="theme-toggle"
  role="switch"
  aria-checked={isLight}
  aria-label="Toggle light and dark theme"
  onclick={toggleTheme}
>
  <span class="track" class:light={isLight}>
    <span class="thumb">
      {#if isLight}
        <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="4.2" />
          <path d="M12 2v2M12 20v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2 12h2M20 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4" />
        </svg>
      {:else}
        <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor" aria-hidden="true">
          <path d="M19 14.5A8 8 0 0 1 9.5 5a.7.7 0 0 0-.94-.86A9 9 0 1 0 19.86 15.4a.7.7 0 0 0-.86-.9z" />
        </svg>
      {/if}
    </span>
  </span>
</button>

<style>
  .theme-toggle {
    display: inline-flex;
    align-items: center;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    outline: none;
  }
  .theme-toggle:focus-visible .track {
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
  .track {
    position: relative;
    width: 52px;
    height: 28px;
    border-radius: 999px;
    background: var(--color-surface-2);
    border: 1px solid var(--color-border);
    transition:
      background var(--duration-base) var(--ease-out-quad),
      border-color var(--duration-base) var(--ease-out-quad),
      box-shadow var(--duration-fast);
  }
  .track.light {
    background: var(--gradient-aurora-soft);
    border-color: color-mix(in srgb, var(--color-accent) 40%, var(--color-border));
  }
  .thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--color-accent-fg);
    background: var(--gradient-aurora);
    box-shadow: 0 2px 8px -2px var(--color-accent-glow);
    transition: transform var(--duration-base) var(--ease-out-expo);
  }
  .track.light .thumb {
    transform: translateX(24px);
  }
  @media (prefers-reduced-motion: reduce) {
    .thumb,
    .track {
      transition: none;
    }
  }
</style>
