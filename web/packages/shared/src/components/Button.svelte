<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';

  type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';
  type Size = 'sm' | 'md' | 'lg';

  type Props = HTMLButtonAttributes & {
    variant?: Variant;
    size?: Size;
    loading?: boolean;
    fullWidth?: boolean;
    children?: Snippet;
    icon?: Snippet;
  };

  let {
    variant = 'primary',
    size = 'md',
    loading = false,
    fullWidth = false,
    disabled,
    children,
    icon,
    class: klass = '',
    ...rest
  }: Props = $props();

  const base = 'inline-flex items-center justify-center gap-2 font-medium select-none transition-all duration-150 ease-out outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--color-bg)] disabled:opacity-50 disabled:cursor-not-allowed active:scale-[0.98]';

  const sizes: Record<Size, string> = {
    sm: 'h-8 px-3 text-[13px] rounded-[var(--radius-sm)]',
    md: 'h-10 px-4 text-sm rounded-[var(--radius-md)]',
    lg: 'h-12 px-6 text-[15px] rounded-[var(--radius-lg)]',
  };

  const variants: Record<Variant, string> = {
    primary:
      'bg-[var(--color-accent)] text-[var(--color-accent-fg)] shadow-[0_1px_0_0_rgba(255,255,255,0.08)_inset,0_4px_18px_-6px_var(--color-accent-glow)] hover:bg-[var(--color-accent-hover)] active:bg-[var(--color-accent-active)]',
    secondary:
      'bg-[var(--color-surface-2)] text-[var(--color-fg)] border border-[var(--color-border)] hover:bg-[color-mix(in_srgb,var(--color-surface-2)_70%,var(--color-accent-soft))] hover:border-[color-mix(in_srgb,var(--color-border)_60%,var(--color-accent))]',
    ghost:
      'bg-transparent text-[var(--color-fg-soft)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-fg)]',
    danger:
      'bg-[var(--color-danger)] text-white hover:brightness-110',
  };
</script>

<button
  class="{base} {sizes[size]} {variants[variant]} {fullWidth ? 'w-full' : ''} {klass}"
  disabled={disabled || loading}
  {...rest}
>
  {#if loading}
    <span class="spinner" aria-hidden="true"></span>
  {:else if icon}
    {@render icon()}
  {/if}
  {#if children}{@render children()}{/if}
</button>

<style>
  .spinner {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 2px solid currentColor;
    border-right-color: transparent;
    animation: spin 0.7s linear infinite;
    opacity: 0.85;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
