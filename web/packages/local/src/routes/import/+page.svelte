<script lang="ts">
  import { Card, Button } from '@engelos/shared/components';
  import { api, ApiException, toast, activeWorkspace } from '@engelos/shared/lib';

  type Source = '' | 'nightbot' | 'streamelements' | 'moobot';

  type ImportResult = {
    channel: string;
    commands_imported: number;
    timers_imported: number;
    skipped: string[];
  };

  const SOURCES: { value: Source; label: string }[] = [
    { value: '', label: 'Auto-detect' },
    { value: 'nightbot', label: 'Nightbot' },
    { value: 'streamelements', label: 'StreamElements' },
    { value: 'moobot', label: 'Moobot' },
  ];

  let channel = $state('');
  let source = $state<Source>('');
  let data = $state('');
  let submitting = $state(false);
  let dataError = $state('');
  let result = $state<ImportResult | null>(null);

  const canSubmit = $derived(data.trim().length > 0 && !submitting);

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) { channel = slug; }
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    dataError = '';
    const payload = data.trim();
    if (!payload) { dataError = 'Paste your export first.'; return; }

    result = null;
    submitting = true;
    try {
      const res = await api.post<ImportResult>('/api/v1/migrate', {
        channel,
        source,
        data: payload,
      });
      result = res;
      const total = res.commands_imported + res.timers_imported;
      toast(`Imported ${total} item${total === 1 ? '' : 's'}.`, 'success');
    } catch (err) {
      handleError(err, 'import the export');
    } finally {
      submitting = false;
    }
  }

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon. Is it running on :8080?', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session abgelaufen, bitte neu anmelden.', 'error', 6000);
      } else {
        toast(err.message || `Failed to ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Failed to ${action}.`, 'error', 6000);
    }
  }
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Import</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Bring your commands and timers over from Nightbot, StreamElements or Moobot.
    </p>
  </header>

  {#if !channel}
    <Card class="reveal-up reveal-up-delay-1">
      <p class="text-[13px] text-fg-soft">Waehle oben einen Workspace, um zu importieren.</p>
    </Card>
  {/if}

  {#if channel}
    <Card class="reveal-up reveal-up-delay-2">
      <form onsubmit={submit} class="space-y-4">
        <label class="block">
          <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
            From which bot
          </span>
          <select class="select" bind:value={source}>
            {#each SOURCES as s (s.value)}
              <option value={s.value}>{s.label}</option>
            {/each}
          </select>
          <span class="block text-[12px] text-muted mt-1.5">
            {#if source === 'moobot'}
              Moobot exports carry no reliable signature, so pick it here explicitly.
            {:else}
              Auto-detect handles Nightbot and StreamElements. For Moobot, choose it explicitly.
            {/if}
          </span>
        </label>

        <label class="block">
          <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
            Export data
          </span>
          <textarea
            class="textarea"
            rows="12"
            placeholder="Paste the contents of your old bot's export file here."
            bind:value={data}
          ></textarea>
          {#if dataError}
            <span class="block text-[12px] text-[var(--color-danger)] mt-1.5">{dataError}</span>
          {:else}
            <span class="block text-[12px] text-muted mt-1.5">
              Channel <span class="font-mono text-accent">{channel}</span>. Existing commands with the same name are kept.
            </span>
          {/if}
        </label>

        <div class="flex items-center justify-end pt-3 border-t border-soft">
          <Button type="submit" loading={submitting} disabled={!canSubmit}>
            {#snippet children()}Import{/snippet}
          </Button>
        </div>
      </form>
    </Card>
  {/if}

  {#if result}
    <Card class="reveal-up">
      <h3 class="text-[15px] font-semibold tracking-tight text-fg-strong">
        Imported {result.commands_imported} command{result.commands_imported === 1 ? '' : 's'}
        and {result.timers_imported} timer{result.timers_imported === 1 ? '' : 's'}.
      </h3>
      {#if result.skipped.length > 0}
        <div class="mt-3">
          <p class="text-[12px] uppercase tracking-wider text-muted mb-1.5">
            Skipped ({result.skipped.length})
          </p>
          <ul class="space-y-1">
            {#each result.skipped as note, i (i)}
              <li class="text-[13px] text-fg-soft">{note}</li>
            {/each}
          </ul>
        </div>
      {:else}
        <p class="text-[13px] text-fg-soft mt-1">Nothing skipped.</p>
      {/if}
    </Card>
  {/if}
</section>

<style>
  .select,
  .textarea {
    display: block;
    width: 100%;
    border-radius: var(--radius-md);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 14px;
    outline: none;
    transition: border-color 150ms, background 150ms;
  }
  .select {
    height: 40px;
    padding: 0 12px;
  }
  .textarea {
    padding: 10px 12px;
    resize: vertical;
    min-height: 200px;
    line-height: 1.5;
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .select:focus,
  .textarea:focus {
    border-color: var(--color-accent);
    background: var(--color-surface);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
</style>
