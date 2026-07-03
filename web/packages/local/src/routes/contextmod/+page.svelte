<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge } from '@engelos/shared/components';
  import { channelApi, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';
  import AiModNav from '$lib/AiModNav.svelte';

  type Config = {
    channel: string;
    enabled: boolean;
    rules: string;
    updated_at: string;
  };

  const MAX_RULES = 4000;

  let channel = $state('');
  let enabled = $state(false);
  let rules = $state('');
  let loading = $state(false);
  let saving = $state(false);
  let loaded = $state(false);

  let remaining = $derived(MAX_RULES - rules.length);

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; void load(); }
  });

  onMount(() => { if (channel) void load(); });

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('Context moderation is not enabled on this bot.', 'warn', 6000);
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  async function load() {
    if (!channel) { return; }
    loading = true;
    try {
      const c = await channelApi(channel).get<Config>('/contextmod');
      enabled = c.enabled;
      rules = c.rules;
      loaded = true;
    } catch (err) {
      handleError(err, 'load context-mod settings');
    } finally {
      loading = false;
    }
  }

  async function save() {
    if (!channel) { return; }
    if (rules.length > MAX_RULES) {
      toast(`Rules are too long (max ${MAX_RULES} characters).`, 'warn');
      return;
    }
    saving = true;
    try {
      await channelApi(channel).put<Config>('/contextmod', { enabled, rules });
      toast('Context-mod settings saved.', 'success');
      loaded = true;
    } catch (err) {
      handleError(err, 'save context-mod settings');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">AI-Mod</p>
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">AI Escalation · Context Moderation</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      The AI second layer. When configured and enabled, it reviews messages that
      pass the fast path against your channel rules for context the deterministic
      rules missed. Describe your rules in plain language; the AI only flags clear
      violations, never acts on mods or the broadcaster, and fails open when unsure.
    </p>
  </header>

  <AiModNav />

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Before this does anything</h3>
    <p class="text-[12.5px] text-fg-soft mb-3">
      AI escalation is a prepared framework, not a proven live system. It stays
      idle until <strong>both</strong> of these are true:
    </p>
    <ol class="prereq">
      <li>
        <span class="prereq-n">1</span>
        <div>
          <p class="prereq-t">Channel rules below are filled in</p>
          <p class="prereq-d">Plain-language rules give the AI something to judge against. Empty rules mean there is nothing to do.</p>
        </div>
      </li>
      <li>
        <span class="prereq-n">2</span>
        <div>
          <p class="prereq-t">An AI backend is configured on this bot</p>
          <p class="prereq-d">Without a configured model backend, escalation cannot run. Enabling the toggle below does not by itself prove a backend is reachable.</p>
        </div>
      </li>
    </ol>
  </Card>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Fail-open, always</h3>
    <p class="text-[12.5px] text-fg-soft mb-3">
      The AI step only ever adds a second opinion. None of these become an automatic punishment:
    </p>
    <ul class="failopen">
      <li>Backend unavailable or rate-limited: the verdict is "unknown" and the message keeps its existing outcome.</li>
      <li>Low-confidence judgement: recorded for review (audit-only), never auto-actioned.</li>
      <li>Unparseable answer: treated as "unknown", no action taken.</li>
      <li>Moderators and the broadcaster are never actioned.</li>
    </ul>
  </Card>

  {#if !channel}
    <Card class="reveal-up reveal-up-delay-1">
      <p class="text-[13px] text-fg-soft">Waehle oben einen Workspace, um die Kontext-Moderation zu konfigurieren.</p>
    </Card>
  {/if}

  {#if loaded}
    <Card class="reveal-up reveal-up-delay-2">
      <div class="flex items-start justify-between mb-1">
        <h3 class="text-[14px] font-semibold tracking-tight text-fg">Context moderation</h3>
        <Badge tone={enabled ? 'accent' : 'neutral'}>{enabled ? 'Enabled' : 'Off'}</Badge>
      </div>
      <p class="text-[12.5px] text-fg-soft mb-5">
        Enabling here expresses intent. Escalation still needs your rules below and
        a configured AI backend (see above) to actually run; with either missing it
        stays idle.
      </p>

      <label class="flex items-center justify-between py-2 border-b border-soft cursor-pointer">
        <span class="text-[13px] text-fg">Enable AI escalation</span>
        <input type="checkbox" bind:checked={enabled} class="h-4 w-4 accent-[var(--color-accent)]" />
      </label>

      <label class="block mt-5">
        <span class="block text-[12.5px] text-fg-soft mb-1.5">Channel rules (plain language)</span>
        <textarea
          bind:value={rules}
          rows="6"
          placeholder="No slurs, no threats, no doxxing, no spam. Keep it family-friendly."
          class="contextmod-input resize-y"
        ></textarea>
        <span class="block text-[11.5px] mt-1" class:text-fg-soft={remaining >= 0} class:text-danger={remaining < 0}>
          {remaining} characters left
        </span>
      </label>
    </Card>

    <div class="flex justify-end gap-2 pt-2 reveal-up reveal-up-delay-3">
      <Button onclick={save} disabled={saving}>
        {#snippet children()}{saving ? 'Saving...' : 'Save changes'}{/snippet}
      </Button>
    </div>
  {/if}
</section>

<style>
  .contextmod-input {
    width: 100%;
    padding: 9px 11px;
    border-radius: var(--radius-md);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 13px;
    line-height: 1.5;
  }
  .contextmod-input:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .prereq {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .prereq li {
    display: flex;
    gap: 11px;
  }
  .prereq-n {
    flex-shrink: 0;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--color-accent-soft);
    color: var(--color-accent);
    font-size: 12px;
    font-weight: 600;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .prereq-t {
    font-size: 13px;
    font-weight: 600;
    color: var(--color-fg);
  }
  .prereq-d {
    font-size: 12px;
    color: var(--color-fg-soft);
    margin-top: 2px;
    line-height: 1.5;
  }
  .failopen {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .failopen li {
    position: relative;
    padding-left: 18px;
    font-size: 12.5px;
    color: var(--color-fg-soft);
    line-height: 1.5;
  }
  .failopen li::before {
    content: '✓';
    position: absolute;
    left: 0;
    top: 0;
    color: var(--color-success);
    font-weight: 700;
    font-size: 12px;
  }
</style>
