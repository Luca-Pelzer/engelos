<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  type Props = HTMLAttributes<HTMLDivElement> & {
    interactive?: boolean;
    padded?: boolean;
    children?: Snippet;
  };

  let {
    interactive = false,
    padded = true,
    class: klass = '',
    children,
    ...rest
  }: Props = $props();
</script>

<div
  class="
    rounded-[var(--radius-lg)] border border-[var(--color-border)]
    bg-[var(--color-surface)]
    {padded ? 'p-5' : ''}
    {interactive ? 'transition-all duration-200 ease-out hover:border-[color-mix(in_srgb,var(--color-border)_50%,var(--color-accent))] hover:-translate-y-[1px] hover:shadow-[var(--shadow-md)] cursor-pointer' : ''}
    {klass}
  "
  {...rest}
>
  {#if children}{@render children()}{/if}
</div>
