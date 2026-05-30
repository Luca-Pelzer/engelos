<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge, Input, EmptyState } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';

  // Binding mirrors the wire shape produced by bindingJSON in
  // internal/api/handlers/redemptions.go. Keep field names in sync with that
  // handler — the dashboard is the only writer of these bindings.
  type Binding = {
    reward_id: string;
    reward_title: string;
    action_type: ActionType;
    action_param: string;
    enabled: boolean;
    auto_fulfill: boolean;
    created_at: string;
    updated_at: string;
  };

  // ActionType is the closed set validated by knownActions in
  // internal/redemptions/store.go. A new action means adding it here AND there.
  type ActionType = 'chat_message' | 'counter_increment' | 'counter_reset' | 'none';

  type ListResponse = { channel: string; bindings: Binding[] };

  const ACTIONS: { value: ActionType; label: string; paramLabel: string; paramHint: string }[] = [
    {
      value: 'chat_message',
      label: 'Send a chat message',
      paramLabel: 'Message template',
      paramHint: 'Use $user, $input, $reward, $cost. Max 480 chars.',
    },
    {
      value: 'counter_increment',
      label: 'Increment a counter',
      paramLabel: 'Counter name',
      paramHint: 'The counter to bump by 1 (e.g. deaths).',
    },
    {
      value: 'counter_reset',
      label: 'Reset a counter',
      paramLabel: 'Counter name',
      paramHint: 'The counter to reset to 0 (e.g. deaths).',
    },
    {
      value: 'none',
      label: 'Do nothing (record only)',
      paramLabel: '',
      paramHint: 'No parameter — the redemption is logged but no action runs.',
    },
  ];

  const CHANNEL_KEY = 'engelos.redemptions.channel';

  let channel = $state('');
  let bindings = $state<Binding[]>([]);
  let loading = $state(false);
  let loaded = $state(false);

  // Form state. editingRewardID is null in create mode, or the reward_id of
  // the binding being edited (in which case reward_id is locked because it is
  // the primary key the API addresses via the URL path).
  let formOpen = $state(false);
  let editingRewardID = $state<string | null>(null);
  let saving = $state(false);
  let fRewardID = $state('');
  let fRewardTitle = $state('');
  let fActionType = $state<ActionType>('chat_message');
  let fActionParam = $state('');
  let fAutoFulfill = $state(false);
  let fEnabled = $state(true);
  let fErrors = $state<{ reward_id?: string; action_param?: string }>({});

  const activeAction = $derived(ACTIONS.find((a) => a.value === fActionType) ?? ACTIONS[0]);
  const paramRequired = $derived(fActionType !== 'none');

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
        `/api/v1/redemptions?channel=${encodeURIComponent(ch)}`,
      );
      bindings = res.bindings ?? [];
      loaded = true;
      localStorage.setItem(CHANNEL_KEY, ch);
    } catch (err) {
      handleError(err, 'load reward bindings');
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    editingRewardID = null;
    fRewardID = '';
    fRewardTitle = '';
    fActionType = 'chat_message';
    fActionParam = '';
    fAutoFulfill = false;
    fEnabled = true;
    fErrors = {};
    formOpen = true;
  }

  function openEdit(b: Binding) {
    editingRewardID = b.reward_id;
    fRewardID = b.reward_id;
    fRewardTitle = b.reward_title;
    fActionType = b.action_type;
    fActionParam = b.action_param;
    fAutoFulfill = b.auto_fulfill;
    fEnabled = b.enabled;
    fErrors = {};
    formOpen = true;
  }

  function closeForm() {
    formOpen = false;
    editingRewardID = null;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    fErrors = {};
    const rewardID = fRewardID.trim();
    const param = fActionParam.trim();
    if (!editingRewardID && !rewardID) fErrors.reward_id = 'Required';
    if (paramRequired && !param) fErrors.action_param = 'Required for this action';
    if (param.length > 480) fErrors.action_param = 'Max 480 characters';
    if (Object.keys(fErrors).length > 0) return;

    const ch = channel.trim().toLowerCase();
    saving = true;
    try {
      if (editingRewardID) {
        await api.put(`/api/v1/redemptions/${encodeURIComponent(editingRewardID)}`, {
          channel: ch,
          reward_title: fRewardTitle.trim(),
          action_type: fActionType,
          action_param: paramRequired ? param : '',
          enabled: fEnabled,
          auto_fulfill: fAutoFulfill,
        });
        toast('Binding updated.', 'success');
      } else {
        await api.post('/api/v1/redemptions', {
          channel: ch,
          reward_id: rewardID,
          reward_title: fRewardTitle.trim(),
          action_type: fActionType,
          action_param: paramRequired ? param : '',
          enabled: fEnabled,
          auto_fulfill: fAutoFulfill,
        });
        toast('Binding created.', 'success');
      }
      closeForm();
      await load();
    } catch (err) {
      handleError(err, 'save the binding');
    } finally {
      saving = false;
    }
  }

  async function toggleEnabled(b: Binding) {
    const ch = channel.trim().toLowerCase();
    const next = !b.enabled;
    try {
      await api.post(`/api/v1/redemptions/${encodeURIComponent(b.reward_id)}/enabled`, {
        channel: ch,
        enabled: next,
      });
      // Optimistic local update so the toggle feels instant without a refetch.
      bindings = bindings.map((x) =>
        x.reward_id === b.reward_id ? { ...x, enabled: next } : x,
      );
      toast(next ? 'Binding enabled.' : 'Binding disabled.', 'success');
    } catch (err) {
      handleError(err, 'toggle the binding');
    }
  }

  async function remove(b: Binding) {
    if (!confirm(`Delete the binding for "${b.reward_title || b.reward_id}"?`)) return;
    const ch = channel.trim().toLowerCase();
    try {
      await api.delete(
        `/api/v1/redemptions/${encodeURIComponent(b.reward_id)}?channel=${encodeURIComponent(ch)}`,
      );
      bindings = bindings.filter((x) => x.reward_id !== b.reward_id);
      toast('Binding deleted.', 'success');
    } catch (err) {
      handleError(err, 'delete the binding');
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

  function actionLabel(t: ActionType): string {
    return ACTIONS.find((a) => a.value === t)?.label ?? t;
  }
</script>

<section class="space-y-6">
  <header class="flex items-end justify-between gap-4 reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Channel Points</h2>
      <p class="text-[13px] text-fg-soft mt-1 max-w-2xl">
        Bind a Twitch Channel-Points reward to a bot action. Requires an
        Affiliate or Partner channel. The broadcaster must Login with Twitch so
        the bot can fulfill redemptions.
      </p>
    </div>
    {#if loaded}
      <Button onclick={openCreate}>
        {#snippet icon()}
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        {/snippet}
        {#snippet children()}New binding{/snippet}
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
      {#if bindings.length === 0}
        <EmptyState
          title="No bindings yet"
          description="Map a reward to a chat message or counter action to get started."
        >
          {#snippet actions()}
            <Button onclick={openCreate}>
              {#snippet children()}Create the first binding{/snippet}
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <table class="w-full text-left">
          <thead>
            <tr class="text-[11px] uppercase tracking-wider text-muted">
              <th class="px-5 py-3 font-medium">Reward</th>
              <th class="px-5 py-3 font-medium">Action</th>
              <th class="px-5 py-3 font-medium">Parameter</th>
              <th class="px-5 py-3 font-medium">Auto</th>
              <th class="px-5 py-3 font-medium">State</th>
              <th class="px-5 py-3 font-medium text-right"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[var(--color-border-soft)]">
            {#each bindings as b (b.reward_id)}
              <tr class="hover:bg-[var(--color-bg-soft)] transition-colors">
                <td class="px-5 py-3.5">
                  <div class="text-[13px] text-fg font-medium">{b.reward_title || '—'}</div>
                  <div class="font-mono text-[11px] text-muted truncate max-w-[180px]">{b.reward_id}</div>
                </td>
                <td class="px-5 py-3.5 text-[13px] text-fg-soft">{actionLabel(b.action_type)}</td>
                <td class="px-5 py-3.5 text-[13px] text-fg-soft max-w-[280px] truncate font-mono">
                  {b.action_param || '—'}
                </td>
                <td class="px-5 py-3.5">
                  {#if b.auto_fulfill}
                    <Badge tone="accent">Fulfill</Badge>
                  {:else}
                    <Badge tone="neutral">Manual</Badge>
                  {/if}
                </td>
                <td class="px-5 py-3.5">
                  {#if b.enabled}
                    <Badge tone="success">Enabled</Badge>
                  {:else}
                    <Badge tone="neutral">Disabled</Badge>
                  {/if}
                </td>
                <td class="px-5 py-3.5 text-right whitespace-nowrap">
                  <button
                    class="text-[12px] text-fg-soft hover:text-accent transition-colors"
                    onclick={() => toggleEnabled(b)}
                  >
                    {b.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <span class="text-border-soft mx-1.5">·</span>
                  <button
                    class="text-[12px] text-fg-soft hover:text-accent transition-colors"
                    onclick={() => openEdit(b)}
                  >
                    Edit
                  </button>
                  <span class="text-border-soft mx-1.5">·</span>
                  <button
                    class="text-[12px] text-fg-soft hover:text-[var(--color-danger)] transition-colors"
                    onclick={() => remove(b)}
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
      aria-label={editingRewardID ? 'Edit binding' : 'New binding'}
    >
      <form onsubmit={save} class="p-6 space-y-4">
        <header class="mb-1">
          <h3 class="text-[16px] font-semibold tracking-tight text-fg-strong">
            {editingRewardID ? 'Edit binding' : 'New binding'}
          </h3>
          <p class="text-[12.5px] text-fg-soft mt-0.5">
            Channel <span class="font-mono text-accent">{channel.trim().toLowerCase()}</span>
          </p>
        </header>

        <Input
          label="Reward ID"
          placeholder="Twitch custom reward UUID"
          bind:value={fRewardID}
          error={fErrors.reward_id}
          hint={editingRewardID
            ? 'Reward ID is the key and cannot be changed.'
            : 'The custom reward UUID from Twitch.'}
          disabled={!!editingRewardID}
        />

        <Input
          label="Reward title (label)"
          placeholder="Optional — for display only"
          bind:value={fRewardTitle}
        />

        <label class="block">
          <span class="block text-[13px] font-medium text-[var(--color-fg-soft)] mb-1.5 tracking-tight">
            Action
          </span>
          <select class="select" bind:value={fActionType}>
            {#each ACTIONS as a (a.value)}
              <option value={a.value}>{a.label}</option>
            {/each}
          </select>
        </label>

        {#if paramRequired}
          <Input
            label={activeAction.paramLabel}
            placeholder={fActionType === 'chat_message' ? 'Thanks $user for redeeming $reward!' : 'deaths'}
            bind:value={fActionParam}
            error={fErrors.action_param}
            hint={activeAction.paramHint}
          />
        {:else}
          <p class="text-[12.5px] text-muted">{activeAction.paramHint}</p>
        {/if}

        <div class="flex items-center gap-5 pt-1">
          <label class="flex items-center gap-2 text-[13px] text-fg-soft cursor-pointer">
            <input type="checkbox" bind:checked={fAutoFulfill} class="checkbox" />
            Auto-fulfill redemption
          </label>
          <label class="flex items-center gap-2 text-[13px] text-fg-soft cursor-pointer">
            <input type="checkbox" bind:checked={fEnabled} class="checkbox" />
            Enabled
          </label>
        </div>

        <div class="flex items-center justify-end gap-2.5 pt-3 border-t border-soft">
          <Button type="button" variant="ghost" onclick={closeForm}>
            {#snippet children()}Cancel{/snippet}
          </Button>
          <Button type="submit" loading={saving}>
            {#snippet children()}{editingRewardID ? 'Save changes' : 'Create binding'}{/snippet}
          </Button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  .select {
    display: block;
    width: 100%;
    height: 40px;
    padding: 0 12px;
    border-radius: var(--radius-md);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border);
    color: var(--color-fg);
    font-size: 14px;
    outline: none;
    transition: border-color 150ms, background 150ms;
  }
  .select:focus {
    border-color: var(--color-accent);
    background: var(--color-surface);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
  .checkbox {
    width: 16px;
    height: 16px;
    accent-color: var(--color-accent);
    cursor: pointer;
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
