<script lang="ts">
  import { Card } from '@engelos/shared/components';

  // The Workflows pillar is one coherent automation surface. This overview
  // frames the documented pipeline (WORKFLOW_BUILDER.md:14-20) and indexes the
  // existing surfaces that feed it. STATIC framing only — no backend calls.
  //
  // Honest scope: the pipeline is trigger -> condition -> action. EngelOS does
  // NOT yet persist or expose workflow run history / per-run audit / telemetry
  // (research engelos-post-sc4-audit-and-workflow-coherence.md §47-60), so this
  // page never claims run logs, audit trails, or run inspection.

  const steps = [
    { n: 1, title: 'Trigger', desc: 'Something starts the workflow: a chat command, a recurring schedule, a Channel-Points redemption, or another Twitch event.' },
    { n: 2, title: 'Condition', desc: 'Optional checks decide whether the workflow should run — role gates, regex, stream state, time windows, or a prior step’s output.' },
    { n: 3, title: 'Action', desc: 'The workflow acts: send chat, bump a counter, fulfill a redemption, call a service, or run an optional AI step. Mods and the broadcaster stay in control.' },
  ];

  // Each card frames an EXISTING surface and links straight to it. Copy is kept
  // in step with each page’s own header/sub-label so nothing implies a new
  // capability.
  const surfaces = [
    {
      title: 'Workflow Builder',
      href: '/actions',
      badge: '/actions',
      desc: 'The rule builder. Wire a trigger to optional conditions and one or more actions, then save it as a rule.',
      cta: 'Open builder',
    },
    {
      title: 'Command Triggers',
      href: '/commands',
      badge: '/commands',
      desc: 'Chat commands that start a workflow, with role gates and templated responses using $user, $channel and $args.',
      cta: 'Manage commands',
    },
    {
      title: 'Schedule Triggers',
      href: '/timers',
      badge: '/timers',
      desc: 'Timer-based starts that fire a workflow on a recurring schedule — not a standalone bot feature.',
      cta: 'Manage schedules',
    },
    {
      title: 'Redemption Triggers',
      href: '/redemptions',
      badge: '/redemptions',
      desc: 'Bind native Twitch Channel-Points redemptions to workflow actions. Needs an Affiliate or Partner channel.',
      cta: 'Manage redemptions',
    },
    {
      title: 'Twitch Rewards',
      href: '/rewards',
      badge: '/rewards',
      desc: 'Define the native Twitch reward entries that redemption triggers listen for, then bind them to a workflow.',
      cta: 'Manage rewards',
    },
    {
      title: 'Counter Actions',
      href: '/counters',
      badge: '/counters',
      desc: 'Named tallies like deaths, wins or fails that workflow actions can read and update as shared state.',
      cta: 'Manage counters',
    },
  ];
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">Pillar 2</p>
    <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">Workflows</h2>
    <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
      Connect what happens on your channel to what should happen next. Every
      workflow is a trigger, optional conditions, and one or more actions —
      built in the Workflow Builder and fed by the trigger and action surfaces
      below.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[15px] font-semibold tracking-tight text-fg mb-1">The workflow pipeline</h3>
    <p class="text-[12.5px] text-fg-soft mb-5">
      Every workflow follows the same shape. Build and edit them in the Workflow Builder.
    </p>
    <ol class="pipeline">
      {#each steps as s (s.n)}
        <li class="step">
          <span class="step-num">{s.n}</span>
          <div>
            <p class="step-title">{s.title}</p>
            <p class="step-desc">{s.desc}</p>
          </div>
        </li>
      {/each}
    </ol>
  </Card>

  <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
    {#each surfaces as surface, i (surface.href)}
      <Card class="reveal-up reveal-up-delay-{Math.min(i + 1, 5)}">
        <div class="flex items-center justify-between mb-1">
          <h3 class="text-[15px] font-semibold tracking-tight text-fg">{surface.title}</h3>
          <span class="badge">{surface.badge}</span>
        </div>
        <p class="text-[12.5px] text-fg-soft mb-4">{surface.desc}</p>
        <a class="link" href={surface.href} data-sveltekit-preload-data="hover">{surface.cta} →</a>
      </Card>
    {/each}
  </div>
</section>

<style>
  .pipeline {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
  }
  .step {
    display: flex;
    gap: 14px;
    padding: 12px 0;
    border-bottom: 1px solid var(--color-border-soft);
  }
  .step:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
  .step:first-child {
    padding-top: 0;
  }
  .step-num {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: var(--color-accent-soft);
    color: var(--color-accent);
    font-size: 12.5px;
    font-weight: 600;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .step-title {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--color-fg);
  }
  .step-desc {
    font-size: 12.5px;
    color: var(--color-fg-soft);
    margin-top: 2px;
  }
  .badge {
    font-size: 11px;
    font-family: var(--font-mono, monospace);
    color: var(--color-muted);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: 2px 7px;
  }
  .link {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-accent);
    text-decoration: none;
    transition: opacity 150ms;
  }
  .link:hover {
    opacity: 0.8;
  }
</style>
