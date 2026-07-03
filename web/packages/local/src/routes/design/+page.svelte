<script lang="ts">
  import { Card, Button, Badge, Input, StatusDot, EmptyState, Skeleton } from '@engelos/shared/components';

  // Living design reference (Phase D1.2c). Internal page: reachable behind
  // operator auth, deliberately NOT linked from the nav. Every token and
  // component variant renders here so visual changes are reviewable on one
  // screen instead of by clicking through the whole app.

  const surfaces = [
    { name: 'bg', v: 'var(--color-bg)' },
    { name: 'bg-soft', v: 'var(--color-bg-soft)' },
    { name: 'surface', v: 'var(--color-surface)' },
    { name: 'surface-2', v: 'var(--color-surface-2)' },
    { name: 'border', v: 'var(--color-border)' },
    { name: 'border-soft', v: 'var(--color-border-soft)' },
  ];
  const fgs = [
    { name: 'fg-strong', v: 'var(--color-fg-strong)' },
    { name: 'fg', v: 'var(--color-fg)' },
    { name: 'fg-soft', v: 'var(--color-fg-soft)' },
    { name: 'muted', v: 'var(--color-muted)' },
  ];
  const accents = [
    { name: 'accent', v: 'var(--color-accent)' },
    { name: 'accent-hover', v: 'var(--color-accent-hover)' },
    { name: 'accent-soft', v: 'var(--color-accent-soft)' },
    { name: 'accent-2', v: 'var(--color-accent-2)' },
  ];
  const status = [
    { name: 'success', v: 'var(--color-success)' },
    { name: 'warn', v: 'var(--color-warn)' },
    { name: 'danger', v: 'var(--color-danger)' },
    { name: 'info', v: 'var(--color-info)' },
  ];
  const radii = ['xs', 'sm', 'md', 'lg', 'xl', '2xl'];
  const shadows = ['sm', 'md', 'lg', 'glow'];
  const badgeTones = ['neutral', 'accent', 'success', 'warn', 'danger', 'info'] as const;
  const dotStates = ['online', 'connecting', 'warn', 'offline'] as const;
  const buttonVariants = ['primary', 'secondary', 'ghost', 'danger'] as const;
</script>

<section class="space-y-8">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">Internal reference</p>
    <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">Design System</h2>
    <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
      Canonical tokens and components. If a surface in the app does not look
      like this page, the surface is wrong. Toggle theme and accent in the top
      bar to review every combination.
    </p>
  </header>

  <Card class="reveal-up">
    <h3 class="sec">Color · surfaces &amp; text</h3>
    <div class="swatch-grid">
      {#each surfaces as s (s.name)}
        <div class="swatch">
          <span class="chip" style="background:{s.v}"></span>
          <span class="chip-name">{s.name}</span>
        </div>
      {/each}
    </div>
    <div class="swatch-grid mt-3">
      {#each fgs as s (s.name)}
        <div class="swatch">
          <span class="chip chip-text" style="color:{s.v}">Ag</span>
          <span class="chip-name">{s.name}</span>
        </div>
      {/each}
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Color · accent (follows the picker) &amp; status</h3>
    <div class="swatch-grid">
      {#each [...accents, ...status] as s (s.name)}
        <div class="swatch">
          <span class="chip" style="background:{s.v}"></span>
          <span class="chip-name">{s.name}</span>
        </div>
      {/each}
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Typography</h3>
    <div class="space-y-3">
      <p class="text-2xl font-semibold tracking-tight text-fg-strong">Display — the streaming control room</p>
      <p class="text-[15px] font-semibold text-fg">Heading — AI-Mod reviewed 412 messages</p>
      <p class="text-[13px] text-fg-soft">Body — messages that pass the fast path can be reviewed by the AI for context the rules missed.</p>
      <p class="text-[12px] text-muted">Caption — since last restart</p>
      <p class="font-mono text-[12.5px] text-fg-soft">mono — engelos --version → v0.79.0-alpha.24</p>
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Radii &amp; shadows</h3>
    <div class="row">
      {#each radii as r (r)}
        <div class="radius-demo" style="border-radius: var(--radius-{r})"><span>{r}</span></div>
      {/each}
    </div>
    <div class="row mt-4">
      {#each shadows as s (s)}
        <div class="shadow-demo" style="box-shadow: var(--shadow-{s})"><span>{s}</span></div>
      {/each}
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Buttons</h3>
    {#each buttonVariants as v (v)}
      <div class="row mb-3">
        <span class="row-label">{v}</span>
        <Button variant={v} size="sm">Small</Button>
        <Button variant={v}>Medium</Button>
        <Button variant={v} size="lg">Large</Button>
        <Button variant={v} loading>Loading</Button>
        <Button variant={v} disabled>Disabled</Button>
      </div>
    {/each}
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Badges &amp; status dots</h3>
    <div class="row">
      {#each badgeTones as t (t)}
        <Badge tone={t}>{t}</Badge>
      {/each}
      <Badge tone="neutral" mono>mono</Badge>
    </div>
    <div class="row mt-4">
      {#each dotStates as s (s)}
        <span class="dot-demo"><StatusDot state={s} /> {s}</span>
      {/each}
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Inputs</h3>
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
      <Input label="Default" placeholder="Type something" hint="Helpful hint below the field." />
      <Input label="With value" value="engelswtf" />
      <Input label="With error" value="not-an-email" error="Enter a valid email address." />
    </div>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Empty state (canonical — use this, not ad-hoc divs)</h3>
    <EmptyState title="No events yet" description="Events appear here as soon as a platform is connected and chat starts flowing.">
      {#snippet actions()}
        <Button size="sm">Connect a platform</Button>
        <Button variant="ghost" size="sm">Learn more</Button>
      {/snippet}
    </EmptyState>
  </Card>

  <Card class="reveal-up">
    <h3 class="sec">Skeletons (canonical loading state)</h3>
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div class="space-y-3">
        <Skeleton width="40%" height="18px" />
        <Skeleton lines={3} />
      </div>
      <div class="flex items-center gap-3">
        <Skeleton width="42px" height="42px" rounded="50%" />
        <div class="flex-1"><Skeleton lines={2} /></div>
      </div>
    </div>
  </Card>
</section>

<style>
  .sec {
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--color-fg);
    margin-bottom: 14px;
  }
  .swatch-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 10px;
  }
  .swatch {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .chip {
    height: 44px;
    border-radius: var(--radius-md);
    border: 1px solid var(--color-border-soft);
  }
  .chip-text {
    display: grid;
    place-items: center;
    font-size: 18px;
    font-weight: 600;
    background: var(--color-surface);
  }
  .chip-name {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--color-muted);
  }
  .row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
  }
  .row-label {
    width: 82px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--color-muted);
  }
  .radius-demo {
    width: 64px;
    height: 48px;
    background: var(--color-surface-2);
    border: 1px solid var(--color-border);
    display: grid;
    place-items: center;
  }
  .radius-demo span,
  .shadow-demo span {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--color-muted);
  }
  .shadow-demo {
    width: 84px;
    height: 56px;
    background: var(--color-surface);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
  }
  .dot-demo {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 12.5px;
    color: var(--color-fg-soft);
  }
</style>
