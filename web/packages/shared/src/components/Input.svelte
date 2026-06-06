<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  type Props = HTMLInputAttributes & {
    label?: string;
    hint?: string;
    error?: string;
    value?: string;
  };

  let {
    label,
    hint,
    error,
    value = $bindable(''),
    id,
    class: klass = '',
    ...rest
  }: Props = $props();

  const inputId = id ?? `in-${Math.random().toString(36).slice(2, 9)}`;
</script>

<label for={inputId} class="block {klass}">
  {#if label}
    <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
      {label}
    </span>
  {/if}
  <input
    id={inputId}
    bind:value
    class="
      block w-full h-10 px-3 rounded-[var(--radius-md)]
      bg-[var(--color-bg-soft)]
      border border-[var(--color-border)]
      text-sm text-[var(--color-fg)] placeholder:text-[var(--color-muted)]
      transition-colors duration-150
      outline-none
      focus:border-[var(--color-accent)] focus:bg-[var(--color-surface)]
      focus:shadow-[0_0_0_3px_var(--color-accent-soft)]
      {error ? 'border-[var(--color-danger)] focus:border-[var(--color-danger)] focus:shadow-[0_0_0_3px_color-mix(in_srgb,var(--color-danger)_20%,transparent)]' : ''}
    "
    {...rest}
  />
  {#if error}
    <span class="block text-[12px] text-[var(--color-danger)] mt-1.5">{error}</span>
  {:else if hint}
    <span class="block text-[12px] text-[var(--color-muted)] mt-1.5">{hint}</span>
  {/if}
</label>
