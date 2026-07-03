<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Badge } from '@engelos/shared/components';
  import { api } from '@engelos/shared/lib';
  import AiModNav from '$lib/AiModNav.svelte';

  // In-memory AI usage counters since process start, for the compact card
  // below. Fetched once on mount; any error just leaves the empty state.
  type AiUsage = {
    calls: number;
    ok: number;
    errors: number;
    tokens_in: number;
    tokens_out: number;
    cache_read_tokens: number;
  };
  let usage = $state<AiUsage | null>(null);
  let usageLoaded = $state(false);

  onMount(async () => {
    try {
      usage = await api.get<AiUsage>('/api/v1/ai/usage');
    } catch {
      usage = null;
    } finally {
      usageLoaded = true;
    }
  });

  // The AI-Mod pillar is one coherent moderation surface. This overview
  // frames the documented pipeline (ENGEL_OS_PRODUCT_CORE.md:32-41) and links
  // the two config sub-views reachable via the shared sub-nav:
  //   - Fast Path (/automod): deterministic rules that act instantly.
  //   - AI Escalation (/contextmod): the AI second opinion that reviews what
  //     the fast path let pass, fail-open when the model is unavailable.

  const steps = [
    { n: 1, title: 'Chat / Event', desc: 'A message or Twitch event enters the moderation stream.' },
    { n: 2, title: 'Fast Path', desc: 'Deterministic rules (Caps, Links, Banned Words, …) decide instantly. Hits are acted on right away.' },
    { n: 3, title: 'AI Escalation', desc: 'Optional. Once channel rules and an AI backend are configured, messages that pass the fast path can be reviewed by the AI for context the rules missed. Until then this stage is skipped.' },
    { n: 4, title: 'Action', desc: 'In active mode a confirmed violation becomes a Twitch-native action: delete, timeout, or ban. In shadow (dry-run) the action is only recorded, never carried out. Mods and the broadcaster are always exempt.' },
    { n: 5, title: 'Audit + Feedback', desc: 'Every decision is logged. Repeat offenders climb an escalation ladder; uncertain cases are recorded, not punished.' },
  ];

  const failOpen = [
    'When the AI backend is down or rate-limited, messages are NOT punished — the verdict becomes "unknown" and existing behaviour is kept.',
    'Low-confidence AI judgements are audit-only: recorded for review, never auto-actioned.',
    'The fast path keeps working independently of the AI backend.',
  ];

  // Honest readiness snapshot. This area is a prepared framework plus a
  // rule-based fast path — not a proven live AI system. These rows say so
  // plainly so the operator knows exactly what is and isn't working yet.
  const readiness: { label: string; tone: 'success' | 'warn' | 'neutral'; status: string; note: string }[] = [
    {
      label: 'Fast Path',
      tone: 'success',
      status: 'Ready',
      note: 'Deterministic rules you configure. Works today in any mode — start it in Dry-run to watch it before it acts.',
    },
    {
      label: 'AI Escalation',
      tone: 'warn',
      status: 'Needs setup',
      note: 'Requires channel rules AND a configured AI backend (set it under AI Backend). Until both exist it stays idle — a prepared framework here, not a proven live system.',
    },
    {
      label: 'Audit log',
      tone: 'neutral',
      status: 'Empty until traffic',
      note: 'Decisions only appear once chat events are processed. An empty log right now is expected, not a fault.',
    },
  ];
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">Pillar 1</p>
    <h2 class="text-2xl font-semibold tracking-tight text-fg-strong">AI-Mod</h2>
    <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
      Chat moderation in two layers: a deterministic fast path you configure, and
      an optional AI escalation step that only runs once channel rules and a
      backend are set up. Both run fail-open — an outage never turns into wrongful
      punishment — and shadow (dry-run) is the recommended way to start.
    </p>
  </header>

  <AiModNav />

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[15px] font-semibold tracking-tight text-fg mb-1">The moderation pipeline</h3>
    <p class="text-[12.5px] text-fg-soft mb-5">
      Every message flows through these stages. Configure the two active stages below.
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

  <Card class="reveal-up reveal-up-delay-2">
    <h3 class="text-[15px] font-semibold tracking-tight text-fg mb-1">Shadow readiness</h3>
    <p class="text-[12.5px] text-fg-soft mb-4">
      Where this area actually stands today. Honest status beats a green
      dashboard — nothing here is enforcing live chat until you turn it on.
    </p>
    <ul class="readiness">
      {#each readiness as r (r.label)}
        <li class="readiness-row">
          <div class="readiness-head">
            <span class="readiness-label">{r.label}</span>
            <Badge tone={r.tone}>{r.status}</Badge>
          </div>
          <p class="readiness-note">{r.note}</p>
        </li>
      {/each}
    </ul>
    <div class="recommend">
      <span class="recommend-tag">Recommended first step</span>
      <p>
        Run in <strong>Shadow (Dry-run)</strong>. It records what would happen
        without timing anyone out, so you can confirm the rules behave before any
        enforcement. Don't enable automatic timeouts or bans until the shadow log
        looks right.
      </p>
    </div>
  </Card>

  <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[15px] font-semibold tracking-tight text-fg">Fast Path</h3>
        <span class="badge">/automod</span>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-4">
        Instant, deterministic rules. Caps, symbols, links, emotes, length,
        repetition, and banned words. Runs first on every message and acts in
        milliseconds — no model round-trip.
      </p>
      <a class="link" href="/automod" data-sveltekit-preload-data="hover">Configure fast path →</a>
    </Card>

    <Card class="reveal-up reveal-up-delay-3">
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[15px] font-semibold tracking-tight text-fg">AI Escalation</h3>
        <span class="badge">/contextmod</span>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-4">
        The AI second opinion. Messages that pass the fast path are reviewed
        against your plain-language channel rules. It only acts on clear
        violations and always fails open when unsure.
      </p>
      <a class="link" href="/contextmod" data-sveltekit-preload-data="hover">Configure AI escalation →</a>
    </Card>

    <Card class="reveal-up reveal-up-delay-3">
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[15px] font-semibold tracking-tight text-fg">AI Backend</h3>
        <span class="badge">/ai-mod/settings</span>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-4">
        The model behind the AI features. Anthropic, OpenAI, Groq — or fully
        local via Ollama. Bring your own key (stored encrypted) and test the
        connection with one click.
      </p>
      <a class="link" href="/ai-mod/settings" data-sveltekit-preload-data="hover">Configure AI backend →</a>
    </Card>

    <Card class="reveal-up reveal-up-delay-3">
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[15px] font-semibold tracking-tight text-fg">Knowledge Base</h3>
        <span class="badge">/kb</span>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-4">
        Teach the bot your rules, schedule, games, FAQ and lore. The kb:lookup
        workflow node retrieves the best entries so the AI answers viewer
        questions with your facts — try the kb-answer template for !ask.
      </p>
      <a class="link" href="/kb" data-sveltekit-preload-data="hover">Manage knowledge →</a>
    </Card>
  </div>

  <Card class="reveal-up reveal-up-delay-4">
    <div class="flex items-center justify-between mb-1">
      <h3 class="text-[15px] font-semibold tracking-tight text-fg">AI usage since start</h3>
      <span class="badge">/ai/usage</span>
    </div>
    {#if !usageLoaded}
      <p class="text-[12.5px] text-fg-soft">Loading…</p>
    {:else if usage && usage.calls > 0}
      <div class="usage-stats">
        <div class="usage-stat"><span class="usage-num">{usage.calls}</span><span class="usage-lbl">calls</span></div>
        <div class="usage-stat"><span class="usage-num">{usage.errors}</span><span class="usage-lbl">errors</span></div>
        <div class="usage-stat"><span class="usage-num">{usage.tokens_in}</span><span class="usage-lbl">tokens in</span></div>
        <div class="usage-stat"><span class="usage-num">{usage.tokens_out}</span><span class="usage-lbl">tokens out</span></div>
        <div class="usage-stat"><span class="usage-num">{usage.cache_read_tokens}</span><span class="usage-lbl">cache reads</span></div>
      </div>
      <p class="text-[11px] text-fg-soft mt-3">In-memory only — resets when the bot restarts.</p>
    {:else}
      <p class="text-[12.5px] text-fg-soft">No AI calls yet</p>
    {/if}
  </Card>

  <Card class="reveal-up reveal-up-delay-4">
    <h3 class="text-[15px] font-semibold tracking-tight text-fg mb-1">Fail-open by design</h3>
    <p class="text-[12.5px] text-fg-soft mb-4">
      Moderation must never punish users because of an outage. These guarantees hold at all times.
    </p>
    <ul class="fail-open-list">
      {#each failOpen as item, i (i)}
        <li>{item}</li>
      {/each}
    </ul>
  </Card>
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
  .fail-open-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .fail-open-list li {
    position: relative;
    padding-left: 18px;
    font-size: 12.5px;
    color: var(--color-fg-soft);
    line-height: 1.5;
  }
  .fail-open-list li::before {
    content: '✓';
    position: absolute;
    left: 0;
    top: 0;
    color: var(--color-success);
    font-weight: 700;
    font-size: 12px;
  }
  .readiness {
    list-style: none;
    margin: 0 0 18px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
  }
  .readiness-row {
    padding: 12px 0;
    border-bottom: 1px solid var(--color-border-soft);
  }
  .readiness-row:first-child {
    padding-top: 0;
  }
  .readiness-row:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
  .readiness-head {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .readiness-label {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--color-fg);
  }
  .readiness-note {
    font-size: 12.5px;
    color: var(--color-fg-soft);
    margin-top: 3px;
    line-height: 1.5;
  }
  .recommend {
    border: 1px solid var(--color-border-soft);
    background: var(--color-bg-soft);
    border-radius: var(--radius-md);
    padding: 12px 14px;
  }
  .recommend-tag {
    display: inline-block;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--color-accent);
    margin-bottom: 4px;
  }
  .recommend p {
    font-size: 12.5px;
    color: var(--color-fg-soft);
    line-height: 1.55;
    margin: 0;
  }
  .recommend strong {
    color: var(--color-fg);
    font-weight: 600;
  }
  .usage-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 22px;
  }
  .usage-stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .usage-num {
    font-size: 18px;
    font-weight: 600;
    color: var(--color-fg);
    font-variant-numeric: tabular-nums;
  }
  .usage-lbl {
    font-size: 11px;
    color: var(--color-fg-soft);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
</style>
