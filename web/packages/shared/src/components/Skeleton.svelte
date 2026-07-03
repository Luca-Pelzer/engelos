<script lang="ts">
  type Props = {
    width?: string;
    height?: string;
    rounded?: string;
    lines?: number;
    class?: string;
  };

  let {
    width = '100%',
    height = '14px',
    rounded = 'var(--radius-sm)',
    lines = 1,
    class: klass = '',
  }: Props = $props();
</script>

{#if lines > 1}
  <div class="skeleton-stack {klass}" aria-hidden="true">
    {#each Array(lines) as _, i (i)}
      <span
        class="skeleton"
        style="height:{height};border-radius:{rounded};width:{i === lines - 1 ? '60%' : width}"
      ></span>
    {/each}
  </div>
{:else}
  <span
    class="skeleton {klass}"
    style="width:{width};height:{height};border-radius:{rounded}"
    aria-hidden="true"
  ></span>
{/if}

<style>
  .skeleton-stack {
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .skeleton {
    display: block;
    background: linear-gradient(
      100deg,
      var(--color-surface-2) 40%,
      color-mix(in srgb, var(--color-fg) 7%, var(--color-surface-2)) 50%,
      var(--color-surface-2) 60%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.4s var(--ease-in-out-quad) infinite;
  }
  @media (prefers-reduced-motion: reduce) {
    .skeleton {
      animation: none;
      background: var(--color-surface-2);
    }
  }
  @keyframes skeleton-shimmer {
    from {
      background-position: 200% 0;
    }
    to {
      background-position: -200% 0;
    }
  }
</style>
