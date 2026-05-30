<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge, Input, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  type Command = {
    id: string;
    name: string;
    response: string;
    min_role: MinRole;
    created_by: string;
    created_at: string;
    updated_at: string;
  };

  type MinRole = 'everyone' | 'subscriber' | 'vip' | 'moderator' | 'broadcaster';

  type ListResponse = { channel: string; commands: Command[] };

  const ROLES: { value: MinRole; label: string }[] = [
    { value: 'everyone', label: 'Everyone' },
    { value: 'subscriber', label: 'Subscribers' },
    { value: 'vip', label: 'VIPs' },
    { value: 'moderator', label: 'Moderators' },
    { value: 'broadcaster', label: 'Broadcaster only' },
  ];

  const ROLE_TONE: Record<MinRole, 'neutral' | 'accent' | 'info' | 'warn' | 'danger'> = {
    everyone: 'neutral',
    subscriber: 'info',
    vip: 'accent',
    moderator: 'warn',
    broadcaster: 'danger',
  };

  const CHANNEL_KEY = 'engelos.commands.channel';

  let channel = $state('');
  let commands = $state<Command[]>([]);
  let loading = $state(false);
  let loaded = $state(false);

  let formOpen = $state(false);
  let editingName = $state<string | null>(null);
  let saving = $state(false);
  let fName = $state('');
  let fResponse = $state('');
  let fMinRole = $state<MinRole>('everyone');
  let fErrors = $state<{ name?: string; response?: string }>({});

  const remaining = $derived(480 - fResponse.length);

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
        `/api/v1/commands?channel=${encodeURIComponent(ch)}`,
      );
      commands = res.commands ?? [];
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load commands');
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    editingName = null;
    fName = '';
    fResponse = '';
    fMinRole = 'everyone';
    fErrors = {};
    formOpen = true;
  }

  function openEdit(c: Command) {
    editingName = c.name;
    fName = c.name;
    fResponse = c.response;
    fMinRole = c.min_role;
    fErrors = {};
    formOpen = true;
  }

  function closeForm() {
    formOpen = false;
    editingName = null;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    fErrors = {};
    const name = fName.trim().toLowerCase().replace(/^!/, '');
    const response = fResponse.trim();
    if (!editingName && !name) fErrors.name = 'Required';
    if (!editingName && name && !/^[a-z0-9_]+$/.test(name)) {
      fErrors.name = 'Only a-z, 0-9 and underscore';
    }
    if (!response) fErrors.response = 'Required';
    if (response.length > 480) fErrors.response = 'Max 480 characters';
    if (Object.keys(fErrors).length > 0) return;

    const ch = channel.trim().toLowerCase();
    saving = true;
    try {
      if (editingName) {
        await api.put(`/api/v1/commands/${encodeURIComponent(editingName)}`, {
          channel: ch,
          response,
          min_role: fMinRole,
        });
        toast('Command updated.', 'success');
      } else {
        await api.post('/api/v1/commands', {
          channel: ch,
          name,
          response,
          min_role: fMinRole,
        });
        toast('Command created.', 'success');
      }
      closeForm();
      await load();
    } catch (err) {
      handleError(err, 'save the command');
    } finally {
      saving = false;
    }
  }

  async function remove(c: Command) {
    if (!confirm(`Delete !${c.name}?`)) return;
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(
        `/api/v1/commands/${encodeURIComponent(c.name)}?channel=${encodeURIComponent(ch)}`,
      );
      commands = commands.filter((x) => x.name !== c.name);
      toast('Command deleted.', 'success');
    } catch (err) {
      handleError(err, 'delete the command');
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

  function roleLabel(r: MinRole): string {
    return ROLES.find((x) => x.value === r)?.label ?? r;
  }
</script>

<section class="space-y-6">
  <header class="flex items-end justify-between gap-4 reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Commands</h2>
      <p class="text-[13px] text-fg-soft mt-1">
        Text replies with role gates. Use $user, $channel and $args in responses.
      </p>
    </div>
    {#if loaded}
      <Button onclick={openCreate}>
        {#snippet icon()}
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        {/snippet}
        {#snippet children()}New command{/snippet}
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
    <Card padded={false} class="reveal-up reveal-up-delay-2">
      {#if commands.length === 0}
        <EmptyState
          title="No commands yet"
          description="Even a simple !discord makes the bot feel alive."
        >
          {#snippet actions()}
            <Button onclick={openCreate}>
              {#snippet children()}Create the first command{/snippet}
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <table class="w-full text-left">
          <thead>
            <tr class="text-[11px] uppercase tracking-wider text-muted">
              <th class="px-5 py-3 font-medium">Trigger</th>
              <th class="px-5 py-3 font-medium">Response</th>
              <th class="px-5 py-3 font-medium">Min role</th>
              <th class="px-5 py-3 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[var(--color-border-soft)]">
            {#each commands as c (c.name)}
              <tr class="hover:bg-[var(--color-bg-soft)] transition-colors">
                <td class="px-5 py-3.5">
                  <span class="font-mono text-[13px] text-accent">!{c.name}</span>
                </td>
                <td class="px-5 py-3.5 text-[13px] text-fg-soft max-w-[420px] truncate">
                  {c.response}
                </td>
                <td class="px-5 py-3.5">
                  <Badge tone={ROLE_TONE[c.min_role]}>{roleLabel(c.min_role)}</Badge>
                </td>
                <td class="px-5 py-3.5 text-right whitespace-nowrap">
                  <button
                    class="text-[12px] text-fg-soft hover:text-accent transition-colors"
                    onclick={() => openEdit(c)}
                  >
                    Edit
                  </button>
                  <span class="text-border-soft mx-1.5">·</span>
                  <button
                    class="text-[12px] text-fg-soft hover:text-[var(--color-danger)] transition-colors"
                    onclick={() => remove(c)}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </Card>
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
      aria-label={editingName ? 'Edit command' : 'New command'}
    >
      <form onsubmit={save} class="p-6 space-y-4">
        <header class="mb-1">
          <h3 class="text-[16px] font-semibold tracking-tight text-fg-strong">
            {editingName ? 'Edit command' : 'New command'}
          </h3>
          <p class="text-[12.5px] text-fg-soft mt-0.5">
            Channel <span class="font-mono text-accent">{channel.trim().toLowerCase()}</span>
          </p>
        </header>

        <Input
          label="Command name"
          placeholder="discord"
          bind:value={fName}
          error={fErrors.name}
          hint={editingName
            ? 'Name is the key and cannot be changed.'
            : 'Without the ! prefix. Letters, numbers, underscore.'}
          disabled={!!editingName}
        />

        <label class="block">
          <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
            Response
          </span>
          <textarea
            class="textarea"
            rows="3"
            placeholder="Hang out with us: discord.gg/yourserver"
            bind:value={fResponse}
          ></textarea>
          <div class="flex items-center justify-between mt-1.5">
            {#if fErrors.response}
              <span class="text-[12px] text-[var(--color-danger)]">{fErrors.response}</span>
            {:else}
              <span class="text-[12px] text-muted">$user, $channel, $args are expanded at send time.</span>
            {/if}
            <span class="text-[12px] tabular-nums {remaining < 0 ? 'text-[var(--color-danger)]' : 'text-muted'}">
              {remaining}
            </span>
          </div>
        </label>

        <label class="block">
          <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
            Minimum role
          </span>
          <select class="select" bind:value={fMinRole}>
            {#each ROLES as r (r.value)}
              <option value={r.value}>{r.label}</option>
            {/each}
          </select>
        </label>

        <div class="flex items-center justify-end gap-2.5 pt-3 border-t border-soft">
          <Button type="button" variant="ghost" onclick={closeForm}>
            {#snippet children()}Cancel{/snippet}
          </Button>
          <Button type="submit" loading={saving}>
            {#snippet children()}{editingName ? 'Save changes' : 'Create command'}{/snippet}
          </Button>
        </div>
      </form>
    </div>
  </div>
{/if}

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
    min-height: 76px;
    line-height: 1.5;
  }
  .select:focus,
  .textarea:focus {
    border-color: var(--color-accent);
    background: var(--color-surface);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
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
    max-width: 460px;
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
