<script lang="ts">
  import { Card, Button, Badge, StatusDot, EmptyState } from '@engelos/shared/components';

  type Stat = { label: string; value: string; delta?: string; tone?: 'up' | 'down' | 'flat' };
  const stats: Stat[] = [
    { label: 'Connected Platforms', value: '2',     delta: 'Twitch · Discord',  tone: 'flat' },
    { label: 'Active Viewers',      value: '47',    delta: '+12 vs. last hour', tone: 'up'   },
    { label: 'Messages Today',      value: '1,284', delta: '+18% week-over-week', tone: 'up' },
    { label: 'Streak Days',         value: '23',    delta: 'Personal best: 41',  tone: 'flat' },
  ];

  type ActivityKind = 'follow' | 'sub' | 'mod' | 'command' | 'clip';
  type Activity = { kind: ActivityKind; who: string; when: string; detail: string };

  const activity: Activity[] = [
    { kind: 'sub',     who: 'kira_dreams',    when: '2m ago',  detail: 'Subscribed for 3 months · Tier 1' },
    { kind: 'command', who: 'engelOS',        when: '4m ago',  detail: 'Ran !so neon_panda → 4 chat reactions' },
    { kind: 'mod',     who: 'AutoMod',        when: '12m ago', detail: 'Timed out raidkid42 (60s · caps)'  },
    { kind: 'follow',  who: 'pixel_witch',    when: '23m ago', detail: 'Followed from Twitch'              },
    { kind: 'clip',    who: 'engelOS',        when: '38m ago', detail: 'Auto-clip created: "that 1v4 ace"' },
  ];

  const kindMeta: Record<ActivityKind, { dot: string; label: string }> = {
    follow:  { dot: 'var(--color-info)',    label: 'Follow' },
    sub:     { dot: 'var(--color-accent)',  label: 'Sub'    },
    mod:     { dot: 'var(--color-warn)',    label: 'Mod'    },
    command: { dot: 'var(--color-muted)',   label: 'Bot'    },
    clip:    { dot: 'var(--color-success)', label: 'Clip'   },
  };
</script>

<section class="space-y-7">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">Welcome back</p>
    <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">
      Your stream's pulse, at a glance.
    </h2>
  </header>

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
    {#each stats as s, i (s.label)}
      <Card class="reveal-up reveal-up-delay-{i + 1}">
        <div class="flex flex-col gap-1">
          <span class="text-[12px] uppercase tracking-wider text-muted font-medium">{s.label}</span>
          <span class="text-[28px] leading-none font-semibold tracking-tight text-fg-strong font-mono mt-1">
            {s.value}
          </span>
          {#if s.delta}
            <span class="text-[12.5px] mt-2 {s.tone === 'up' ? 'text-[var(--color-success)]' : 'text-fg-soft'}">
              {s.delta}
            </span>
          {/if}
        </div>
      </Card>
    {/each}
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-5">
    <Card class="lg:col-span-2 reveal-up reveal-up-delay-5" padded={false}>
      <div class="flex items-center justify-between px-5 py-4 border-b border-soft">
        <div class="flex items-center gap-2">
          <h3 class="text-[14px] font-semibold tracking-tight text-fg">Recent Activity</h3>
          <Badge tone="accent">Live</Badge>
        </div>
        <a href="/chat" class="text-[12.5px] text-fg-soft hover:text-accent transition-colors">
          View all →
        </a>
      </div>
      {#if activity.length === 0}
        <EmptyState
          title="No events yet"
          description="Your community is sleeping. Once you go live, follows, subs and bot actions will stream in here."
        />
      {:else}
        <ul class="divide-y divide-[var(--color-border-soft)]">
          {#each activity as a (a.who + a.when)}
            <li class="flex items-center gap-3.5 px-5 py-3.5 transition-colors hover:bg-[var(--color-bg-soft)]">
              <span class="activity-dot" style="--c: {kindMeta[a.kind].dot}"></span>
              <div class="flex-1 min-w-0">
                <div class="flex items-baseline gap-2">
                  <span class="font-mono text-[13px] text-fg-strong truncate">{a.who}</span>
                  <span class="text-[11px] text-muted uppercase tracking-wider">{kindMeta[a.kind].label}</span>
                </div>
                <p class="text-[12.5px] text-fg-soft truncate">{a.detail}</p>
              </div>
              <time class="text-[12px] text-muted tabular-nums">{a.when}</time>
            </li>
          {/each}
        </ul>
      {/if}
    </Card>

    <Card class="reveal-up reveal-up-delay-5">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Quick Actions</h3>
      <p class="text-[12.5px] text-fg-soft mb-4">Common one-click moves.</p>
      <div class="space-y-2">
        <Button variant="secondary" fullWidth>
          {#snippet children()}Send shout-out{/snippet}
        </Button>
        <Button variant="secondary" fullWidth>
          {#snippet children()}Create command{/snippet}
        </Button>
        <Button variant="secondary" fullWidth>
          {#snippet children()}Run AutoMod check{/snippet}
        </Button>
        <Button variant="secondary" fullWidth>
          {#snippet children()}Open chat viewer{/snippet}
        </Button>
      </div>

      <div class="mt-5 pt-5 border-t border-soft">
        <div class="flex items-center gap-2 text-[12.5px] text-fg-soft">
          <StatusDot state="online" />
          Bot uptime <span class="font-mono text-fg ml-auto">2d 14h</span>
        </div>
      </div>
    </Card>
  </div>
</section>

<style>
  .activity-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--c);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--c) 18%, transparent);
    flex-shrink: 0;
  }
</style>
