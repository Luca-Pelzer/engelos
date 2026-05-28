<script lang="ts">
  import type { Snippet } from 'svelte';

  type Tone = 'neutral' | 'accent' | 'success' | 'warn' | 'danger' | 'info';
  type Props = {
    tone?: Tone;
    children?: Snippet;
    mono?: boolean;
    class?: string;
  };

  let { tone = 'neutral', mono = false, class: klass = '', children }: Props = $props();

  const tones: Record<Tone, string> = {
    neutral: 'bg-[var(--color-surface-2)] text-[var(--color-fg-soft)] border-[var(--color-border)]',
    accent:  'bg-[var(--color-accent-soft)] text-[var(--color-accent-hover)] border-[color-mix(in_srgb,var(--color-accent)_30%,transparent)]',
    success: 'bg-[color-mix(in_srgb,var(--color-success)_15%,transparent)] text-[var(--color-success)] border-[color-mix(in_srgb,var(--color-success)_30%,transparent)]',
    warn:    'bg-[color-mix(in_srgb,var(--color-warn)_15%,transparent)] text-[var(--color-warn)] border-[color-mix(in_srgb,var(--color-warn)_30%,transparent)]',
    danger:  'bg-[color-mix(in_srgb,var(--color-danger)_15%,transparent)] text-[var(--color-danger)] border-[color-mix(in_srgb,var(--color-danger)_30%,transparent)]',
    info:    'bg-[color-mix(in_srgb,var(--color-info)_15%,transparent)] text-[var(--color-info)] border-[color-mix(in_srgb,var(--color-info)_30%,transparent)]',
  };
</script>

<span
  class="
    inline-flex items-center gap-1.5 h-[22px] px-2 rounded-[var(--radius-sm)]
    text-[11px] font-medium uppercase tracking-wider
    border
    {mono ? 'font-mono normal-case tracking-normal' : ''}
    {tones[tone]} {klass}
  "
>
  {#if children}{@render children()}{/if}
</span>
