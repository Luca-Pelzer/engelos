<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Button, Badge, Input } from '@engelos/shared/components';
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import AiModNav from '$lib/AiModNav.svelte';

  // AI Backend settings. Global (not channel-scoped), owner/admin gated.
  // Contract: internal/api/handlers/ai.go + internal/aibackend/aiconfig.
  // "custom" is a UI-only preset: the daemon stores the concrete wire
  // (anthropic|openai) as the provider, so for custom we submit the selected
  // wire together with the operator's base URL.

  type ProviderInfo = {
    id: string;
    label: string;
    auth: 'api_key' | 'none';
    wire?: string;
    wire_options?: string[];
    default_model?: string;
    default_base_url?: string;
    note?: string;
  };
  type Snapshot = {
    provider: string;
    base_url: string;
    model: string;
    api_key_set: boolean;
    api_key_hint?: string;
    source: 'db' | 'env' | 'default';
  };
  type TestResult = {
    ok: boolean;
    latency_ms: number;
    provider: string;
    model: string;
    error?: string;
  };

  let providers = $state<ProviderInfo[]>([]);
  let selected = $state('anthropic'); // catalog id, incl. 'custom'
  let wire = $state('openai'); // custom preset only
  let baseUrl = $state('');
  let model = $state('');
  let apiKey = $state(''); // new key input; empty = keep stored key
  let keySet = $state(false);
  let keyHint = $state('');
  let source = $state<Snapshot['source']>('default');

  let loading = $state(true);
  let saving = $state(false);
  let testing = $state(false);
  let removingKey = $state(false);
  let testResult = $state<TestResult | null>(null);

  // The loaded snapshot, to detect whether the form was modified since load
  // (a modified form is tested as an explicit override, which cannot reuse
  // the encrypted stored key).
  let loadedSnap = $state<Snapshot | null>(null);

  const preset = $derived(providers.find((p) => p.id === selected));
  const needsKey = $derived((preset?.auth ?? 'api_key') === 'api_key');
  const wireOptions = $derived(preset?.wire_options ?? []);

  const sourceLabel: Record<Snapshot['source'], string> = {
    db: 'Configured here',
    env: 'From server env',
    default: 'Built-in defaults',
  };
  const sourceTone: Record<Snapshot['source'], 'success' | 'warn' | 'neutral'> = {
    db: 'success',
    env: 'warn',
    default: 'neutral',
  };

  const submitProvider = $derived(selected === 'custom' ? wire : selected);

  const dirty = $derived(
    loadedSnap === null ||
      submitProvider !== loadedSnap.provider ||
      baseUrl.trim() !== loadedSnap.base_url ||
      model.trim() !== loadedSnap.model ||
      apiKey.trim() !== ''
  );

  function handleError(err: unknown, action: string) {
    if (err instanceof ApiException) {
      if (err.status === 0) {
        toast('Cannot reach the engelOS daemon.', 'error', 6000);
      } else if (err.status === 401) {
        toast('Session expired, sign in again.', 'error', 6000);
      } else if (err.status === 501) {
        toast('AI backend configuration is not enabled on this bot.', 'warn', 6000);
      } else if (
        err.status === 400 &&
        typeof err.details === 'object' &&
        err.details !== null &&
        (err.details as { error?: string }).error === 'secrets_key_required'
      ) {
        toast(
          'The daemon has no ENGELOS_SECRETS_KEY, so API keys cannot be stored encrypted. Set it and restart, then save the key again.',
          'error',
          9000
        );
      } else {
        toast(err.message || `Could not ${action}.`, 'error', 6000);
      }
    } else {
      toast(`Could not ${action}.`, 'error', 6000);
    }
  }

  function applySnapshot(s: Snapshot) {
    loadedSnap = s;
    keySet = s.api_key_set;
    keyHint = s.api_key_hint ?? '';
    source = s.source;
    baseUrl = s.base_url;
    model = s.model;
    apiKey = '';

    // Map the stored provider back onto a catalog preset. groq/ollama are
    // stored under their own ids; a bare wire (anthropic/openai) counts as
    // the plain preset when the base URL matches its default, otherwise it
    // is a custom endpoint on that wire.
    const direct = providers.find((p) => p.id === s.provider && p.id !== 'custom');
    if (direct && (s.provider === 'groq' || s.provider === 'ollama')) {
      selected = direct.id;
      return;
    }
    if (direct) {
      if (!s.base_url || s.base_url === direct.default_base_url) {
        selected = direct.id;
        return;
      }
      selected = 'custom';
      wire = s.provider;
      return;
    }
    selected = 'custom';
    wire = s.provider === 'anthropic' ? 'anthropic' : 'openai';
  }

  function selectPreset(id: string) {
    if (id === selected) return;
    selected = id;
    testResult = null;
    const p = providers.find((x) => x.id === id);
    if (!p) return;
    if (id === 'custom') {
      // Keep whatever the operator had; just make sure the wire is valid.
      if (!wireOptions.includes(wire) && p.wire_options?.length) {
        wire = p.wire_options[0];
      }
      return;
    }
    baseUrl = p.default_base_url ?? '';
    model = p.default_model ?? '';
  }

  async function load() {
    loading = true;
    try {
      const [prov, snap] = await Promise.all([
        api.get<{ providers: ProviderInfo[] }>('/api/v1/ai/providers'),
        api.get<Snapshot>('/api/v1/ai/config'),
      ]);
      providers = prov.providers;
      applySnapshot(snap);
    } catch (err) {
      handleError(err, 'load the AI backend configuration');
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void load();
  });

  async function test() {
    testing = true;
    testResult = null;
    try {
      if (!dirty) {
        // Unchanged form: test the ACTIVE config server-side, which can use
        // the stored (encrypted) key.
        testResult = await api.post<TestResult>('/api/v1/ai/test', {});
      } else {
        if (needsKey && keySet && !apiKey.trim()) {
          toast(
            'Testing unsaved changes cannot use the stored key — enter the API key below or save first.',
            'warn',
            7000
          );
        }
        const body: Record<string, string> = {
          provider: submitProvider,
          base_url: baseUrl.trim(),
          model: model.trim(),
        };
        if (apiKey.trim()) body.api_key = apiKey.trim();
        testResult = await api.post<TestResult>('/api/v1/ai/test', body);
      }
    } catch (err) {
      handleError(err, 'test the AI backend');
    } finally {
      testing = false;
    }
  }

  async function save() {
    saving = true;
    try {
      const body: Record<string, string> = {
        provider: submitProvider,
        base_url: baseUrl.trim(),
        model: model.trim(),
      };
      if (apiKey.trim()) body.api_key = apiKey.trim();
      const snap = await api.put<Snapshot>('/api/v1/ai/config', body);
      applySnapshot(snap);
      testResult = null;
      toast('AI backend saved.', 'success');
    } catch (err) {
      handleError(err, 'save the AI backend configuration');
    } finally {
      saving = false;
    }
  }

  async function removeKey() {
    removingKey = true;
    try {
      const snap = await api.put<Snapshot>('/api/v1/ai/config', {
        provider: submitProvider,
        base_url: baseUrl.trim(),
        model: model.trim(),
        clear_api_key: true,
      });
      applySnapshot(snap);
      testResult = null;
      toast('Stored API key removed.', 'success');
    } catch (err) {
      handleError(err, 'remove the stored API key');
    } finally {
      removingKey = false;
    }
  }
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <p class="text-[13px] text-fg-soft mb-1">AI-Mod</p>
    <div class="flex items-center gap-3">
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">AI Backend</h2>
      {#if !loading}
        <Badge tone={sourceTone[source]}>{sourceLabel[source]}</Badge>
      {/if}
    </div>
    <p class="text-[13px] text-fg-soft mt-1">
      The model that powers AI escalation, translation, co-host replies and clip
      titles. Pick a cloud provider with your own API key, or run fully local —
      keys are encrypted at rest and never leave this bot.
    </p>
  </header>

  <AiModNav />

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">Provider</h3>
    <p class="text-[12.5px] text-fg-soft mb-4">
      Cloud or local — your choice. Everything below runs through the same
      fail-open pipeline: if the backend is unreachable, nothing gets punished.
    </p>

    {#if loading}
      <p class="text-[13px] text-fg-soft">Loading…</p>
    {:else}
      <div class="provider-grid" role="radiogroup" aria-label="AI provider">
        {#each providers as p (p.id)}
          <button
            type="button"
            class="provider"
            class:selected={selected === p.id}
            role="radio"
            aria-checked={selected === p.id}
            onclick={() => selectPreset(p.id)}
          >
            <span class="provider-label">{p.label}</span>
            <span class="provider-auth">{p.auth === 'none' ? 'no key needed' : 'API key'}</span>
          </button>
        {/each}
      </div>

      {#if preset?.note}
        <p class="preset-note">{preset.note}</p>
      {/if}

      <div class="mt-5 space-y-4">
        {#if selected === 'custom' && wireOptions.length}
          <label class="block">
            <span class="block text-[13px] font-medium text-fg-soft mb-1.5 tracking-tight">Wire format</span>
            <select bind:value={wire} class="field-select">
              {#each wireOptions as w (w)}
                <option value={w}>{w === 'openai' ? 'OpenAI-compatible' : 'Anthropic-compatible'}</option>
              {/each}
            </select>
            <span class="block text-[12px] text-muted mt-1.5">
              The request format your endpoint speaks. Ollama, vLLM, LM Studio and
              most self-hosted runners are OpenAI-compatible.
            </span>
          </label>
        {/if}

        <Input
          label="Base URL"
          placeholder={preset?.default_base_url ?? 'https://…'}
          hint="Endpoint root. Leave the preset value unless you proxy the provider."
          bind:value={baseUrl}
        />

        <Input
          label="Model"
          placeholder={preset?.default_model ?? 'model id'}
          hint={preset?.default_model
            ? `Empty uses the provider default (${preset.default_model}).`
            : 'The exact model id this provider should run.'}
          bind:value={model}
        />

        {#if needsKey}
          <div>
            <Input
              label="API key"
              type="password"
              autocomplete="off"
              placeholder={keySet ? 'leave blank to keep the stored key' : 'paste your provider API key'}
              bind:value={apiKey}
            />
            {#if keySet}
              <div class="key-row">
                <span class="key-hint">
                  Key stored encrypted{keyHint ? ` (${keyHint})` : ''} — write-only, the API never returns it.
                </span>
                <Button variant="ghost" size="sm" loading={removingKey} onclick={removeKey}>
                  Remove key
                </Button>
              </div>
            {:else}
              <p class="key-hint mt-1.5">
                Stored AES-256-GCM encrypted on this bot. Write-only: it is never
                shown again after saving.
              </p>
            {/if}
          </div>
        {/if}
      </div>

      <div class="actions">
        <Button variant="secondary" loading={testing} onclick={test}>Test connection</Button>
        <Button loading={saving} onclick={save}>Save</Button>

        {#if testResult}
          {#if testResult.ok}
            <span class="test-ok" role="status">
              ✓ {testResult.provider} · {testResult.model} · {testResult.latency_ms} ms
            </span>
          {:else}
            <span class="test-fail" role="status" title={testResult.error}>
              ✗ {testResult.error || 'Connection failed'}
            </span>
          {/if}
        {/if}
      </div>
    {/if}
  </Card>

  <Card class="reveal-up reveal-up-delay-2">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg mb-1">How this is used</h3>
    <ul class="use-list">
      <li><strong>AI Escalation</strong> reviews borderline chat against your channel rules.</li>
      <li><strong>Translation</strong> turns foreign-language chat into your language.</li>
      <li><strong>Co-Host &amp; Clipper</strong> generate replies and clip titles when enabled.</li>
    </ul>
    <p class="text-[12.5px] text-fg-soft mt-3">
      Every consumer fails open: an unreachable or rate-limited backend never
      causes a timeout or ban, it only skips the AI step.
    </p>
  </Card>
</section>

<style>
  .provider-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 8px;
  }
  .provider {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    border: 1px solid var(--color-border);
    background: var(--color-bg-soft);
    cursor: pointer;
    text-align: left;
    transition:
      border-color 150ms var(--ease-out-expo),
      background 150ms var(--ease-out-expo),
      box-shadow 150ms var(--ease-out-expo);
  }
  .provider:hover {
    border-color: color-mix(in srgb, var(--color-border) 40%, var(--color-accent));
  }
  .provider.selected {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 12%, transparent);
  }
  .provider-label {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--color-fg);
  }
  .provider-auth {
    font-size: 11.5px;
    color: var(--color-muted);
  }
  .preset-note {
    margin-top: 12px;
    font-size: 12.5px;
    color: var(--color-fg-soft);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: 9px 12px;
    line-height: 1.5;
  }
  .field-select {
    display: block;
    width: 100%;
    height: 40px;
    padding: 0 12px;
    border-radius: var(--radius-md);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border);
    font-size: 14px;
    color: var(--color-fg);
    outline: none;
    transition: border-color 150ms;
  }
  .field-select:focus {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 3px var(--color-accent-soft);
  }
  .key-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 6px;
  }
  .key-hint {
    font-size: 12px;
    color: var(--color-muted);
    line-height: 1.5;
  }
  .actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 20px;
    padding-top: 16px;
    border-top: 1px solid var(--color-border-soft);
  }
  .test-ok {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--color-success);
  }
  .test-fail {
    font-size: 12.5px;
    font-weight: 500;
    color: var(--color-danger);
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .use-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .use-list li {
    position: relative;
    padding-left: 18px;
    font-size: 12.5px;
    color: var(--color-fg-soft);
    line-height: 1.5;
  }
  .use-list li::before {
    content: '›';
    position: absolute;
    left: 2px;
    top: 0;
    color: var(--color-accent);
    font-weight: 700;
  }
  .use-list strong {
    color: var(--color-fg);
    font-weight: 600;
  }
</style>
