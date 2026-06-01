<script lang="ts">
  type Props = { size?: number; withWordmark?: boolean };
  let { size = 28, withWordmark = true }: Props = $props();
</script>

<span class="inline-flex items-center gap-2.5 leading-none" style="--logo-size: {size}px">
  <span class="logo-mark" aria-hidden="true">
    <svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="engel-grad" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stop-color="#45f0d6" />
          <stop offset="50%" stop-color="#0fe0c0" />
          <stop offset="100%" stop-color="#ff2e97" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="28" height="28" rx="8" fill="url(#engel-grad)" />
      <path
        d="M10 22 L10 10 L22 10 M10 16 L19 16 M10 22 L22 22"
        stroke="#fff"
        stroke-width="2.4"
        stroke-linecap="round"
        stroke-linejoin="round"
        fill="none"
      />
    </svg>
  </span>
  {#if withWordmark}
    <span class="logo-text">
      engel<span class="logo-text-accent">OS</span>
    </span>
  {/if}
</span>

<style>
  .logo-mark {
    display: inline-flex;
    width: var(--logo-size);
    height: var(--logo-size);
    filter: drop-shadow(0 4px 12px var(--color-accent-glow));
  }
  .logo-mark svg {
    width: 100%;
    height: 100%;
  }
  .logo-text {
    font-family: var(--font-display);
    font-weight: 700;
    letter-spacing: -0.02em;
    font-size: calc(var(--logo-size) * 0.62);
    color: var(--color-fg-strong);
  }
  .logo-text-accent {
    color: var(--color-accent);
    font-weight: 500;
  }
</style>
