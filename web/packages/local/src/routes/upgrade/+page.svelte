<script lang="ts">
  import { Card, Button, Badge } from '@engelos/shared/components';

  type Feature = { icon: string; title: string; copy: string };
  const features: Feature[] = [
    { icon: '◐', title: 'AI Co-Host',           copy: 'Voice-cloned TTS reads chat highlights and reacts in your stream — <300ms latency. Cloud-only.' },
    { icon: '◇', title: 'AI Auto-Clipper',      copy: 'Excitement detection picks the moments, Claude writes titles, clips post themselves.' },
    { icon: '◆', title: 'Cross-Stream Analytics', copy: 'See your community across Twitch, YouTube, Discord and Kick in one ledger.' },
    { icon: '◈', title: 'Unlimited Team Seats', copy: 'Mods, editors, social managers — invite everyone. Granular RBAC.' },
    { icon: '◉', title: 'Automatic Backups',    copy: 'Hourly snapshots, point-in-time recovery, off-region replication.' },
    { icon: '◎', title: 'Multi-Region Hosting', copy: 'Edge-routed control plane. Your viewers connect to the closest node.' },
  ];

  type Tier = {
    name: string;
    price: string;
    period: string;
    blurb: string;
    cta: string;
    featured?: boolean;
    perks: string[];
  };

  const tiers: Tier[] = [
    {
      name: 'Self-Hosted',
      price: '€0',
      period: 'forever',
      blurb: 'The full Core. AGPL-3.0. Yours to keep.',
      cta: 'Already running',
      perks: ['All platforms', 'Loyalty · Streaks · Pity', 'BYOK AI features', 'Community support'],
    },
    {
      name: 'Pro',
      price: '€9.99',
      period: '/ month',
      blurb: 'Managed hosting plus the AI features.',
      cta: 'Upgrade to Pro',
      featured: true,
      perks: ['Everything in Self-Hosted', 'AI Co-Host included', 'Auto-Clipper included', 'Daily backups'],
    },
    {
      name: 'Team',
      price: '€24.99',
      period: '/ month',
      blurb: 'For streamers with mods, editors, growth.',
      cta: 'Talk to us',
      perks: ['Everything in Pro', 'Unlimited seats', 'Cross-Stream Analytics', 'Priority support'],
    },
  ];
</script>

<section class="space-y-12 max-w-[1080px] mx-auto">
  <header class="text-center pt-4 reveal-up">
    <Badge tone="accent" class="mb-4">{#snippet children()}engelOS Cloud{/snippet}</Badge>
    <h2 class="text-[36px] sm:text-[44px] font-semibold tracking-[-0.025em] text-fg-strong leading-[1.05]">
      Self-host the core.<br/>
      <span class="text-accent">Let us host the magic.</span>
    </h2>
    <p class="mt-4 text-[15.5px] text-fg-soft max-w-xl mx-auto leading-relaxed">
      Cloud unlocks features that need GPUs, edge infrastructure, or always-on
      services we can't ship into your binary.
    </p>
  </header>

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
    {#each features as f, i (f.title)}
      <Card class="reveal-up reveal-up-delay-{Math.min(i + 1, 5)}">
        <div class="flex items-start gap-3">
          <span class="feat-glyph">{f.icon}</span>
          <div>
            <h3 class="text-[14px] font-semibold tracking-tight text-fg-strong">{f.title}</h3>
            <p class="text-[12.5px] text-fg-soft mt-1.5 leading-relaxed">{f.copy}</p>
          </div>
        </div>
      </Card>
    {/each}
  </div>

  <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
    {#each tiers as t (t.name)}
      <div class="tier" class:featured={t.featured}>
        {#if t.featured}
          <span class="tier-tag">Most popular</span>
        {/if}
        <div class="tier-head">
          <h3 class="text-[15px] font-semibold tracking-tight text-fg-strong">{t.name}</h3>
          <p class="text-[12.5px] text-fg-soft mt-1">{t.blurb}</p>
        </div>
        <div class="tier-price">
          <span class="font-display text-[34px] font-semibold tracking-tight text-fg-strong">{t.price}</span>
          <span class="text-[13px] text-fg-soft">{t.period}</span>
        </div>
        <ul class="tier-perks">
          {#each t.perks as p (p)}
            <li>
              <svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <path d="M3 8.5 6.5 12 13 4.5" />
              </svg>
              {p}
            </li>
          {/each}
        </ul>
        <div class="mt-auto pt-5">
          <Button variant={t.featured ? 'primary' : 'secondary'} fullWidth disabled={t.name === 'Self-Hosted'}>
            {#snippet children()}{t.cta}{/snippet}
          </Button>
        </div>
      </div>
    {/each}
  </div>

  <p class="text-center text-[12px] text-muted">
    All Cloud features can be cancelled monthly. Your data exports as JSON, anytime.
  </p>
</section>

<style>
  .feat-glyph {
    width: 30px;
    height: 30px;
    border-radius: var(--radius-md);
    background: var(--color-accent-soft);
    color: var(--color-accent);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 16px;
    flex-shrink: 0;
  }
  .tier {
    position: relative;
    display: flex;
    flex-direction: column;
    padding: 24px;
    border-radius: var(--radius-xl);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    transition: transform var(--duration-base) var(--ease-out-expo), border-color var(--duration-base);
  }
  .tier:hover {
    transform: translateY(-2px);
  }
  .tier.featured {
    background:
      radial-gradient(circle at 50% 0%, var(--color-accent-soft) 0%, transparent 60%),
      var(--color-surface);
    border-color: color-mix(in srgb, var(--color-accent) 50%, var(--color-border));
    box-shadow: var(--shadow-glow);
  }
  .tier-tag {
    position: absolute;
    top: -10px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--color-accent);
    color: var(--color-accent-fg);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    padding: 4px 10px;
    border-radius: 999px;
  }
  .tier-head {
    margin-bottom: 18px;
  }
  .tier-price {
    display: flex;
    align-items: baseline;
    gap: 6px;
    margin-bottom: 20px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--color-border-soft);
  }
  .tier-perks {
    display: flex;
    flex-direction: column;
    gap: 9px;
    font-size: 13px;
    color: var(--color-fg);
  }
  .tier-perks li {
    display: flex;
    align-items: center;
    gap: 9px;
  }
  .tier-perks svg {
    color: var(--color-accent);
    flex-shrink: 0;
  }
</style>
