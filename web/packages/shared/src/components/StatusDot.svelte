<script lang="ts">
  type State = 'online' | 'offline' | 'connecting' | 'warn';
  type Props = { state?: State; pulse?: boolean; class?: string };
  let { state = 'online', pulse = true, class: klass = '' }: Props = $props();

  const colors: Record<State, string> = {
    online:     'var(--color-success)',
    offline:    'var(--color-muted)',
    connecting: 'var(--color-warn)',
    warn:       'var(--color-warn)',
  };
</script>

<span
  class="status-dot {klass}"
  class:pulse={pulse && state !== 'offline'}
  style="--dot-color: {colors[state]}"
  aria-label={state}
></span>

<style>
  .status-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--dot-color);
    position: relative;
    flex-shrink: 0;
  }
  .status-dot.pulse::before {
    content: '';
    position: absolute;
    inset: -3px;
    border-radius: 50%;
    background: var(--dot-color);
    opacity: 0.35;
    animation: dot-pulse 1.8s var(--ease-out-quad) infinite;
  }
  @keyframes dot-pulse {
    0%   { transform: scale(1);   opacity: 0.35; }
    70%  { transform: scale(2.2); opacity: 0;    }
    100% { transform: scale(2.2); opacity: 0;    }
  }
</style>
