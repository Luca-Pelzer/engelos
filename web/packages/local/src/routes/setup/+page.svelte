<script lang="ts">
  import { Button, Logo, Badge } from '@engelos/shared/components';

  type Step = { n: number; title: string; sub: string };
  const steps: Step[] = [
    { n: 1, title: 'Owner Account',    sub: 'Email + password. You stay in control.' },
    { n: 2, title: 'Connect Twitch',   sub: 'OAuth — read chat, send messages, manage rewards.' },
    { n: 3, title: 'Connect Discord',  sub: 'Optional. Bridge your community Discord server.' },
    { n: 4, title: 'Done',             sub: 'Bot goes live. The rest is up to you.' },
  ];
</script>

<div class="min-h-screen flex items-center justify-center px-6 py-16 grid-noise relative overflow-hidden">
  <div class="aura" aria-hidden="true"></div>

  <div class="w-full max-w-[640px] relative z-10">
    <div class="flex justify-center mb-8 reveal-up">
      <Logo size={40} />
    </div>

    <div class="text-center reveal-up reveal-up-delay-1">
      <Badge tone="accent" class="mb-5">
        {#snippet children()}First Run{/snippet}
      </Badge>
      <h1 class="text-[34px] sm:text-[40px] font-semibold tracking-[-0.025em] text-fg-strong leading-[1.05]">
        Welcome to engelOS.
      </h1>
      <p class="mt-3 text-[17px] text-fg-soft max-w-[480px] mx-auto leading-relaxed">
        The streaming bot that remembers you.
      </p>
    </div>

    <ol class="mt-12 space-y-1.5 reveal-up reveal-up-delay-2">
      {#each steps as s, i (s.n)}
        <li class="step" style="--i: {i}">
          <span class="step-n">{s.n}</span>
          <div class="step-body">
            <span class="step-title">{s.title}</span>
            <span class="step-sub">{s.sub}</span>
          </div>
          {#if i < steps.length - 1}
            <span class="step-tail" aria-hidden="true"></span>
          {/if}
        </li>
      {/each}
    </ol>

    <div class="mt-10 flex flex-col items-center gap-3 reveal-up reveal-up-delay-3">
      <Button size="lg" href="/setup/owner">
        {#snippet children()}Start setup{/snippet}
      </Button>
      <p class="text-[12.5px] text-muted">
        Takes about <span class="text-fg-soft font-medium">3 minutes</span>.
      </p>
    </div>
  </div>
</div>

<style>
  .aura {
    position: absolute;
    width: 600px;
    height: 600px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    filter: blur(90px);
    top: -200px;
    left: 50%;
    transform: translateX(-50%);
    pointer-events: none;
  }
  .step {
    position: relative;
    display: grid;
    grid-template-columns: 36px 1fr;
    gap: 14px;
    align-items: start;
    padding: 12px 14px;
    border-radius: var(--radius-md);
    transition: background var(--duration-fast);
  }
  .step:hover {
    background: var(--color-surface);
  }
  .step-n {
    grid-column: 1;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--color-fg-soft);
    margin-top: 2px;
    position: relative;
    z-index: 1;
  }
  .step-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-top: 4px;
  }
  .step-title {
    font-size: 14.5px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--color-fg);
  }
  .step-sub {
    font-size: 13px;
    color: var(--color-fg-soft);
  }
  .step-tail {
    position: absolute;
    left: 28px;
    top: 38px;
    bottom: -8px;
    width: 1px;
    background: linear-gradient(180deg, var(--color-border), transparent);
  }
</style>
