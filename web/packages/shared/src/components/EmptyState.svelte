<script lang="ts">
  import type { Snippet } from 'svelte';

  type Props = {
    title: string;
    description?: string;
    children?: Snippet;
    actions?: Snippet;
    illustration?: Snippet;
  };

  let { title, description, children, actions, illustration }: Props = $props();
</script>

<div class="flex flex-col items-center text-center py-10 px-6">
  <div class="empty-art mb-5" aria-hidden="true">
    {#if illustration}
      {@render illustration()}
    {:else}
      <svg viewBox="0 0 80 80" xmlns="http://www.w3.org/2000/svg">
        <circle cx="40" cy="40" r="34" stroke="var(--color-border)" stroke-width="1.5" fill="none" stroke-dasharray="3 5" />
        <circle cx="40" cy="40" r="20" fill="var(--color-accent-soft)" />
        <path d="M30 42 c2 4 6 6 10 6 s8 -2 10 -6" stroke="var(--color-accent)" stroke-width="2" stroke-linecap="round" fill="none" />
        <circle cx="33" cy="36" r="1.6" fill="var(--color-accent)" />
        <circle cx="47" cy="36" r="1.6" fill="var(--color-accent)" />
      </svg>
    {/if}
  </div>
  <h3 class="text-base font-semibold text-[var(--color-fg)] tracking-tight mb-1.5">
    {title}
  </h3>
  {#if description}
    <p class="text-[13.5px] text-[var(--color-fg-soft)] max-w-sm leading-relaxed">
      {description}
    </p>
  {/if}
  {#if children}
    <div class="mt-4 text-[13.5px] text-[var(--color-fg-soft)] max-w-md leading-relaxed">
      {@render children()}
    </div>
  {/if}
  {#if actions}
    <div class="mt-6 flex flex-wrap items-center justify-center gap-2.5">
      {@render actions()}
    </div>
  {/if}
</div>

<style>
  .empty-art {
    width: 80px;
    height: 80px;
    opacity: 0.95;
  }
  .empty-art svg {
    width: 100%;
    height: 100%;
  }
</style>
