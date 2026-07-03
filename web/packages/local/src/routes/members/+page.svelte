<script lang="ts">
  import { channelApi, activeWorkspace, ApiException } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Member = { user_id: string; role: string; source: string };
  type Invitation = { id: string; twitch_login: string; role: string; accepted: boolean; created_at: string };

  let channel = $state('');
  let role = $state('');
  let autoVerify = $state(false);
  let savingToggle = $state(false);
  let members = $state<Member[]>([]);
  let invitations = $state<Invitation[]>([]);
  let loading = $state(true);
  let errorCode = $state('');

  let inviteLogin = $state('');
  let inviting = $state(false);
  let inviteError = $state('');

  const isOwner = $derived(role === 'owner');

  async function load() {
    if (!channel) return;
    loading = true;
    errorCode = '';
    try {
      const ws = await channelApi(channel).get<{ role: string; auto_verify_mods: boolean }>('');
      role = ws.role;
      autoVerify = ws.auto_verify_mods;
      members = (await channelApi(channel).get<{ members: Member[] }>('/members')).members ?? [];
      if (ws.role === 'owner') {
        invitations = (await channelApi(channel).get<{ invitations: Invitation[] }>('/invitations')).invitations ?? [];
      } else {
        invitations = [];
      }
    } catch (err) {
      errorCode = err instanceof ApiException ? (err.status === 404 ? 'not_found' : 'forbidden') : 'error';
    } finally {
      loading = false;
    }
  }

  async function invite(e: Event) {
    e.preventDefault();
    const login = inviteLogin.trim().toLowerCase();
    if (!login || inviting) return;
    inviting = true;
    inviteError = '';
    try {
      await channelApi(channel).post('/invitations', { twitch_login: login });
      inviteLogin = '';
      invitations = (await channelApi(channel).get<{ invitations: Invitation[] }>('/invitations')).invitations ?? [];
    } catch (err) {
      inviteError = err instanceof ApiException && err.status === 409 ? 'Already invited.' : 'Invitation failed.';
    } finally {
      inviting = false;
    }
  }

  async function revokeInvite(id: string) {
    try {
      await channelApi(channel).delete(`/invitations/${encodeURIComponent(id)}`);
      invitations = invitations.filter((i) => i.id !== id);
    } catch { /* keep row on failure */ }
  }

  async function toggleAutoVerify() {
    if (savingToggle) return;
    const next = !autoVerify;
    savingToggle = true;
    try {
      await channelApi(channel).put('/auto-verify', { enabled: next });
      autoVerify = next;
    } catch { /* keep previous state on failure */ }
    finally { savingToggle = false; }
  }

  async function removeMember(userId: string) {
    try {
      await channelApi(channel).delete(`/members/${encodeURIComponent(userId)}`);
      members = members.filter((m) => m.user_id !== userId);
    } catch { /* keep row on failure */ }
  }

  function sourceLabel(s: string): string {
    if (s === 'owner') return 'Owner';
    if (s === 'twitch_verified') return 'Twitch-Mod';
    if (s === 'invite') return 'Invited';
    return s;
  }

  $effect(() => {
    const slug = $activeWorkspace;
    if (slug && slug !== channel) {
      channel = slug;
      void load();
    }
  });

  onMount(() => { if (channel) void load(); });
</script>

<section class="page" data-screen-label="members">
  <div class="page-wrap">
    <header class="head">
      <div>
        <h2>Members</h2>
        {#if channel}<span class="ws-label">@{channel}</span>{/if}
      </div>
    </header>

    {#if loading}
      <div class="muted">Loading…</div>
    {:else if errorCode}
      <div class="empty">
        <div class="t">{errorCode === 'forbidden' ? 'No access' : errorCode === 'not_found' ? 'Channel not found' : 'Error'}</div>
        <div class="d">{errorCode === 'forbidden' ? 'You are not a member of this workspace.' : 'Please select a workspace.'}</div>
      </div>
    {:else}
      {#if isOwner}
        <div class="card toggle-card">
          <div class="toggle-meta">
            <h3>Auto-approve Twitch mods</h3>
            <p class="hint">Anyone who moderates your Twitch channel automatically gets mod access here on login.</p>
          </div>
          <button
            class="switch"
            class:on={autoVerify}
            onclick={toggleAutoVerify}
            disabled={savingToggle}
            role="switch"
            aria-checked={autoVerify}
            aria-label="Auto-approve Twitch mods"
          >
            <span class="knob"></span>
          </button>
        </div>

        <div class="card">
          <h3>Invite a moderator</h3>
          <p class="hint">Invite by Twitch login. They join automatically the next time they sign in.</p>
          <form class="invite-row" onsubmit={invite}>
            <span class="at">@</span>
            <input
              class="inp"
              type="text"
              placeholder="twitch_login"
              bind:value={inviteLogin}
              autocomplete="off"
              spellcheck="false"
            />
            <button class="btn btn-primary" type="submit" disabled={inviting || !inviteLogin.trim()}>
              {inviting ? 'Sending…' : 'Invite'}
            </button>
          </form>
          {#if inviteError}<div class="err">{inviteError}</div>{/if}
        </div>
      {/if}

      <div class="card">
        <h3>Active members <span class="count">{members.length}</span></h3>
        <ul class="rows">
          {#each members as m (m.user_id)}
            <li class="row">
              <span class="avatar" class:owner={m.role === 'owner'}>{(m.role === 'owner' ? 'O' : 'M')}</span>
              <div class="who">
                <span class="rname">{m.role === 'owner' ? 'Owner' : 'Moderator'}</span>
                <span class="rsub">{sourceLabel(m.source)}</span>
              </div>
              {#if isOwner && m.role !== 'owner'}
                <button class="btn-ghost" onclick={() => removeMember(m.user_id)} title="Remove">Remove</button>
              {/if}
            </li>
          {/each}
        </ul>
      </div>

      {#if isOwner}
        <div class="card">
          <h3>Open invitations <span class="count">{invitations.filter((i) => !i.accepted).length}</span></h3>
          {#if invitations.filter((i) => !i.accepted).length === 0}
            <p class="hint">No open invitations.</p>
          {:else}
            <ul class="rows">
              {#each invitations.filter((i) => !i.accepted) as inv (inv.id)}
                <li class="row">
                  <span class="avatar pending">@</span>
                  <div class="who">
                    <span class="rname">@{inv.twitch_login}</span>
                    <span class="rsub">{inv.role === 'mod' ? 'Moderator' : inv.role} - ausstehend</span>
                  </div>
                  <button class="btn-ghost" onclick={() => revokeInvite(inv.id)} title="Zuruecknehmen">Zuruecknehmen</button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</section>

<style>
  .head { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 18px; }
  .head h2 { margin: 0; font-size: 1.3rem; }
  .ws-label { font-size: .85rem; color: var(--text-dim); }
  .muted { color: var(--text-dim); }
  .card { padding: 18px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); margin-bottom: 16px; }
  .card h3 { margin: 0 0 4px; font-size: 1rem; display: flex; align-items: center; gap: 8px; }
  .count { font-size: 11px; font-weight: 700; color: var(--text-dim); background: var(--bg); border: 1px solid var(--panel-border); border-radius: 999px; padding: 1px 8px; }
  .hint { margin: 0 0 12px; font-size: .88rem; color: var(--text-dim); }
  .invite-row { display: flex; align-items: center; gap: 8px; }
  .at { color: var(--text-dim); font-weight: 700; }
  .inp { flex: 1; padding: 9px 12px; border-radius: 10px; background: var(--bg); border: 1px solid var(--panel-border); color: var(--text); font-size: .95rem; }
  .inp:focus { outline: none; border-color: var(--brand); }
  .err { margin-top: 10px; font-size: .85rem; color: #ff6b6b; }
  .rows { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
  .row { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border-radius: 12px; background: var(--bg); border: 1px solid var(--panel-border); }
  .avatar { width: 36px; height: 36px; flex: 0 0 auto; display: grid; place-items: center; border-radius: 10px; font-weight: 800; color: var(--text-dim); background: var(--panel-bg); border: 1px solid var(--panel-border); }
  .avatar.owner { color: #fff; background: linear-gradient(135deg, var(--brand), var(--brand-2)); border: 0; }
  .avatar.pending { color: var(--text-dim); }
  .who { display: flex; flex-direction: column; }
  .rname { font-weight: 600; font-size: .95rem; }
  .rsub { font-size: .78rem; color: var(--text-dim); }
  .btn-ghost { margin-left: auto; font-size: .82rem; font-weight: 600; color: var(--text-dim); background: transparent; border: 1px solid var(--panel-border); border-radius: 8px; padding: 6px 12px; cursor: pointer; }
  .btn-ghost:hover { color: #ff6b6b; border-color: #ff6b6b; }
  .toggle-card { display: flex; align-items: center; gap: 16px; }
  .toggle-meta { flex: 1; }
  .toggle-meta h3 { margin: 0 0 4px; }
  .toggle-meta .hint { margin: 0; }
  .switch { flex: 0 0 auto; width: 46px; height: 26px; border-radius: 999px; border: 1px solid var(--panel-border); background: var(--bg); position: relative; cursor: pointer; transition: background .15s ease, border-color .15s ease; padding: 0; }
  .switch .knob { position: absolute; top: 2px; left: 2px; width: 20px; height: 20px; border-radius: 50%; background: var(--text-dim); transition: transform .15s ease, background .15s ease; }
  .switch.on { background: linear-gradient(135deg, var(--brand), var(--brand-2)); border-color: transparent; }
  .switch.on .knob { transform: translateX(20px); background: #fff; }
  .switch:disabled { opacity: .6; cursor: default; }
  .empty { text-align: center; padding: 40px 20px; }
  .empty .t { font-weight: 700; font-size: 1.05rem; }
  .empty .d { color: var(--text-dim); font-size: .9rem; margin-top: 6px; }
</style>
