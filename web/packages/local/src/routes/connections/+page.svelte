<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, Badge } from '@engelos/shared/components';
  import { api, toast, API_BASE } from '@engelos/shared/lib';

  type Connection = {
    id: string;
    provider: string;
    provider_login: string;
    purpose: string;
    scopes: string[];
    can_create_clip: boolean;
    expires_at: string;
    expired: boolean;
    updated_at: string;
  };

  let connections = $state<Connection[]>([]);
  let loading = $state(true);
  let unlinking = $state('');

  const userLoginUrl = `${API_BASE}/api/v1/auth/twitch/login?purpose=user`;
  const botLoginUrl = `${API_BASE}/api/v1/auth/twitch/login?purpose=bot`;

  function purposeLabel(p: string): string {
    if (p === 'bot') return 'Bot account';
    if (p === 'user') return 'Your account';
    return p;
  }

  function twitchConn(purpose: string): Connection | undefined {
    return connections.find((c) => c.provider === 'twitch' && c.purpose === purpose);
  }

  async function load() {
    loading = true;
    try {
      const res = await api.get<{ connections: Connection[] }>('/api/v1/connections');
      connections = res.connections ?? [];
    } catch {
      toast('Could not load connections.', 'error');
    } finally {
      loading = false;
    }
  }

  async function unlink(conn: Connection) {
    if (!confirm(`Disconnect ${conn.provider} (${conn.provider_login || 'unknown'})? The bot loses this authorization.`)) {
      return;
    }
    unlinking = conn.id;
    try {
      await api.delete(`/api/v1/connections/${encodeURIComponent(conn.id)}`);
      toast('Disconnected.', 'success');
      await load();
    } catch {
      toast('Could not disconnect.', 'error');
    } finally {
      unlinking = '';
    }
  }

  onMount(load);
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Connections</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Link Twitch so the bot can read and write chat, create clips, and manage channel-point
      redemptions. Tokens are stored encrypted and refreshed automatically.
    </p>
  </header>

  {#each [{ purpose: 'bot', url: botLoginUrl, blurb: 'The account the bot posts and clips as. Log in with the bot account here.' }, { purpose: 'user', url: userLoginUrl, blurb: 'Your broadcaster account, used to authorize channel-level actions.' }] as row (row.purpose)}
    {@const conn = twitchConn(row.purpose)}
    <Card class="reveal-up reveal-up-delay-1">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <h3 class="text-[14px] font-semibold tracking-tight text-fg">Twitch</h3>
            <Badge tone="neutral">{purposeLabel(row.purpose)}</Badge>
            {#if conn && !conn.expired}
              <Badge tone="accent">Connected</Badge>
            {:else if conn && conn.expired}
              <Badge tone="warn">Token expired</Badge>
            {:else}
              <Badge tone="neutral">Not connected</Badge>
            {/if}
          </div>
          <p class="text-[12.5px] text-fg-soft mt-1">{row.blurb}</p>

          {#if conn}
            <div class="mt-3 text-[12.5px] text-fg-soft space-y-1">
              <div>
                Signed in as
                <span class="text-fg font-medium">{conn.provider_login || 'unknown'}</span>
              </div>
              <div class="flex items-center gap-2">
                <span>Auto-Clipper clip creation:</span>
                {#if conn.can_create_clip}
                  <Badge tone="accent">Granted</Badge>
                {:else}
                  <Badge tone="warn">Missing clips:edit, re-auth needed</Badge>
                {/if}
              </div>
              {#if conn.scopes?.length}
                <div class="flex flex-wrap gap-1.5 pt-1">
                  {#each conn.scopes as scope (scope)}
                    <span class="scope-chip">{scope}</span>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </div>

        <div class="flex flex-col gap-2 shrink-0">
          <a href={row.url} class="twitch-btn" data-sveltekit-reload>
            <svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor" aria-hidden="true">
              <path d="M4 2L2.5 5.5v13H7V22h3l3-3h4l5-5V2zm15 11l-3 3h-4l-3 3v-3H7V4h12zM15 7h-2v5h2zm-5 0H8v5h2z"/>
            </svg>
            <span>{conn ? 'Re-authorize' : 'Connect'}</span>
          </a>
          {#if conn}
            <button class="unlink-btn" onclick={() => unlink(conn)} disabled={unlinking === conn.id}>
              {unlinking === conn.id ? 'Disconnecting...' : 'Disconnect'}
            </button>
          {/if}
        </div>
      </div>
    </Card>
  {/each}

  {#if loading}
    <p class="text-[12.5px] text-fg-soft reveal-up">Loading connections...</p>
  {/if}
</section>

<style>
  .twitch-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 9px 14px;
    border-radius: var(--radius-md);
    background: #9146ff;
    color: #fff;
    font-size: 13px;
    font-weight: 600;
    transition: background var(--duration-fast) var(--ease-out-quad), transform var(--duration-fast);
  }
  .twitch-btn:hover {
    background: #7c2fff;
    transform: translateY(-1px);
  }
  .unlink-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 7px 14px;
    border-radius: var(--radius-md);
    background: transparent;
    border: 1px solid var(--color-border);
    color: var(--color-fg-soft);
    font-size: 12.5px;
    font-weight: 500;
    cursor: pointer;
    transition: border-color var(--duration-fast), color var(--duration-fast);
  }
  .unlink-btn:hover {
    border-color: var(--color-danger);
    color: var(--color-danger);
  }
  .unlink-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .scope-chip {
    display: inline-block;
    padding: 2px 7px;
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-fg-soft);
    font-size: 11px;
    font-family: var(--font-mono, monospace);
  }
</style>
