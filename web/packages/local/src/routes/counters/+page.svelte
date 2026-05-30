<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Input, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Counter = {
    id: string;
    name: string;
    value: number;
    updated_at: string;
  };

  type ListResponse = { channel: string; counters: Counter[] };

  const CHANNEL_KEY = 'engelos.counters.channel';

  let channel = $state('');
  let counters = $state<Counter[]>([]);
  let loading = $state(false);
  let loaded = $state(false);
  let busy = $state<Record<string, boolean>>({});

  let formOpen = $state(false);
  let saving = $state(false);
  let fName = $state('');
  let fValue = $state('0');
  let fErrors = $state<{ name?: string; value?: string }>({});

  onMount(() => {
    const saved = localStorage.getItem(CHANNEL_KEY);
    if (saved) {
      channel = saved;
      void load();
    }
  });

  async function load() {
    const ch = channel.trim().toLowerCase();
    if (!ch) {
      toast('Enter a channel login first.', 'warn');
      return;
    }
    loading = true;
    try {
      const res = await api.get<ListResponse>(
        `/api/v1/counters?channel=${encodeURIComponent(ch)}`,
      );
      counters = res.counters ?? [];
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load counters');
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    fName = '';
    fValue = '0';
    fErrors = {};
    formOpen = true;
  }

  function closeForm() {
    formOpen = false;
  }

  async function create(e: SubmitEvent) {
    e.preventDefault();
    fErrors = {};
    const name = fName.trim().toLowerCase();
    const value = Number(fValue);
    if (!name) fErrors.name = 'Required';
    else if (!/^[a-z0-9_]+$/.test(name)) fErrors.name = 'Only a-z, 0-9 and underscore';
    else if (name.length > 32) fErrors.name = 'Max 32 characters';
    if (!Number.isInteger(value)) fErrors.value = 'Must be a whole number';
    if (Object.keys(fErrors).length > 0) return;

    const ch = channel.trim().toLowerCase();
    saving = true;
    try {
      await api.put(`/api/v1/counters/${encodeURIComponent(name)}`, {
        channel: ch,
        value,
      });
      toast('Counter created.', 'success');
      closeForm();
      await load();
    } catch (err) {
      handleError(err, 'create the counter');
    } finally {
      saving = false;
    }
  }

  async function bump(c: Counter, delta: number) {
    const ch = channel.trim().toLowerCase();
    busy = { ...busy, [c.name]: true };
    try {
      const updated = await api.post<Counter>(
        `/api/v1/counters/${encodeURIComponent(c.name)}/add`,
        { channel: ch, delta },
      );
      counters = counters.map((x) => (x.name === c.name ? updated : x));
    } catch (err) {
      handleError(err, 'update the counter');
    } finally {
      busy = { ...busy, [c.name]: false };
    }
  }

  async function setValue(c: Counter) {
    const ch = channel.trim().toLowerCase();
    const input = prompt(`Set !${c.name} to:`, String(c.value));
    if (input === null) return;
    const value = Number(input.trim());
    if (!Number.isInteger(value)) {
      toast('Value must be a whole number.', 'warn');
      return;
    }
    busy = { ...busy, [c.name]: true };
    try {
      const updated = await api.put<Counter>(`/api/v1/counters/${encodeURIComponent(c.name)}`, {
        channel: ch,
        value,
      });
      counters = counters.map((x) => (x.name === c.name ? updated : x));
      toast('Counter updated.', 'success');
    } catch (err) {
      handleError(err, 'set the counter');
    } finally {
      busy = { ...busy, [c.name]: false };
    }
  }

  async function remove(c: Counter) {
    if (!confirm(`Delete counter !${c.name}?`)) return;
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(
        `/api/v1/counters/${encodeURIComponent(c.name)}?channel=${encodeURIComponent(ch)}`,
      );
      counters = counters.filter((x) => x.name !== c.name);
      toast('Counter deleted.', 'success');
    } catch (err) {
      handleError(err, 'delete the counter');
    }
  }

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon. Is it running on :8080?', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired — sign in again.', 'error', 6000);
      } else {
        toast(err.message || `Failed to ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Failed to ${action}.`, 'error', 6000);
    }
  }
</script>

<section class="space-y-6">
  <header class="flex items-end justify-between gap-4 reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Counters</h2>
      <p class="text-[13px] text-fg-soft mt-1">
        Named tallies like deaths, wins or fails. Bump them from chat or here.
      </p>
    </div>
    {#if loaded}
      <Button onclick={openCreate}>
        {#snippet icon()}
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        {/snippet}
        {#snippet children()}New counter{/snippet}
      </Button>
    {/if}
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <form
      class="flex items-end gap-3"
      onsubmit={(e) => {
        e.preventDefault();
        void load();
      }}
    >
      <div class="flex-1 max-w-xs">
        <Input
          label="Channel login"
          placeholder="e.g. engelswtf"
          bind:value={channel}
          hint="The broadcaster's Twitch login (lower-cased)."
        />
      </div>
      <Button type="submit" variant="secondary" loading={loading}>
        {#snippet children()}Load{/snippet}
      </Button>
    </form>
  </Card>

  {#if loaded}
    {#if counters.length === 0}
      <Card padded={false} class="reveal-up reveal-up-delay-2">
        <EmptyState
          title="No counters yet"
          description="Create one like deaths or wins, then bump it with !deaths in chat."
        >
          {#snippet actions()}
            <Button onclick={openCreate}>
              {#snippet children()}Create the first counter{/snippet}
            </Button>
          {/snippet}
        </EmptyState>
      </Card>
    {:else}
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {#each counters as c (c.name)}
          <Card class="reveal-up">
            <div class="flex items-start justify-between gap-2">
              <span class="font-mono text-[13px] text-accent truncate">!{c.name}</span>
              <button
                class="text-[12px] text-fg-soft hover:text-[var(--color-danger)] transition-colors shrink-0"
                onclick={() => remove(c)}
              >
                Delete
              </button>
            </div>

            <button
              class="block w-full text-left mt-3 mb-4 group"
              onclick={() => setValue(c)}
              title="Click to set an exact value"
            >
              <span class="text-[40px] leading-none font-semibold tracking-tight text-fg-strong font-mono tabular-nums group-hover:text-accent transition-colors">
                {c.value.toLocaleString('en-US')}
              </span>
            </button>

            <div class="flex items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                disabled={busy[c.name]}
                onclick={() => bump(c, -1)}
              >
                {#snippet children()}−1{/snippet}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={busy[c.name]}
                onclick={() => bump(c, 1)}
              >
                {#snippet children()}+1{/snippet}
              </Button>
              <button
                class="ml-auto text-[12px] text-fg-soft hover:text-accent transition-colors"
                onclick={() => setValue(c)}
              >
                Set…
              </button>
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  {/if}
</section>

<svelte:window
  onkeydown={(e) => {
    if (formOpen && e.key === 'Escape') closeForm();
  }}
/>

{#if formOpen}
  <div class="modal-backdrop">
    <button
      type="button"
      class="modal-scrim"
      aria-label="Close dialog"
      onclick={closeForm}
    ></button>
    <div
      class="modal-panel"
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-label="New counter"
    >
      <form onsubmit={create} class="p-6 space-y-4">
        <header class="mb-1">
          <h3 class="text-[16px] font-semibold tracking-tight text-fg-strong">New counter</h3>
          <p class="text-[12.5px] text-fg-soft mt-0.5">
            Channel <span class="font-mono text-accent">{channel.trim().toLowerCase()}</span>
          </p>
        </header>

        <Input
          label="Counter name"
          placeholder="deaths"
          bind:value={fName}
          error={fErrors.name}
          hint="Without the ! prefix. Letters, numbers, underscore."
        />

        <Input
          label="Starting value"
          type="number"
          bind:value={fValue}
          error={fErrors.value}
          hint="Whole number; may be negative."
        />

        <div class="flex items-center justify-end gap-2.5 pt-3 border-t border-soft">
          <Button type="button" variant="ghost" onclick={closeForm}>
            {#snippet children()}Cancel{/snippet}
          </Button>
          <Button type="submit" loading={saving}>
            {#snippet children()}Create counter{/snippet}
          </Button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    animation: fade-in 150ms var(--ease-out-quad);
  }
  .modal-scrim {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
    cursor: default;
    background: color-mix(in srgb, var(--color-bg) 70%, transparent);
    backdrop-filter: blur(4px);
  }
  .modal-panel {
    position: relative;
    z-index: 1;
    width: 100%;
    max-width: 420px;
    max-height: calc(100vh - 48px);
    overflow-y: auto;
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    box-shadow: var(--shadow-lg);
    animation: panel-in 200ms var(--ease-out-expo);
  }
  @keyframes fade-in {
    from { opacity: 0; }
    to   { opacity: 1; }
  }
  @keyframes panel-in {
    from { opacity: 0; transform: translateY(10px) scale(0.98); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }
</style>
