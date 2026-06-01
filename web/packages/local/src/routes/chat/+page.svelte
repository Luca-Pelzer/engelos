<script lang="ts">
  import { ws, api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount, onDestroy, tick } from 'svelte';

  type ChatMsg = {
    id: string;
    platform: string;
    username: string;
    content: string;
    isMod: boolean;
    isSub: boolean;
    isVip: boolean;
    ts: number;
    state: 'live' | 'deleted' | 'timeout' | 'banned';
  };

  let messages = $state<ChatMsg[]>([]);
  let platform = $state<'all' | 'twitch' | 'youtube' | 'kick'>('all');
  let onlyMentions = $state(false);
  let onlyMods = $state(false);
  let autoScroll = $state(true);
  let connected = $state(false);

  let draft = $state('');
  let target = $state<'all' | 'twitch' | 'youtube' | 'kick'>('all');

  let feedEl: HTMLDivElement;
  const ownName = 'engelgaming';

  const visible = $derived(
    messages.filter((m) => {
      if (platform !== 'all' && m.platform !== platform) return false;
      if (onlyMods && !m.isMod) return false;
      if (onlyMentions && !m.content.toLowerCase().includes('@' + ownName)) return false;
      return true;
    }),
  );

  function fmtTime(ts: number): string {
    return new Date(ts).toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' });
  }

  async function pushAndScroll(m: ChatMsg) {
    messages = [...messages.slice(-499), m];
    if (autoScroll) {
      await tick();
      feedEl?.scrollTo({ top: feedEl.scrollHeight });
    }
  }

  function onWsEvent(ev: { type: string; payload?: unknown }) {
    if (ev.type === 'platform.connected') { connected = true; return; }
    if (ev.type === 'platform.disconnected') { connected = false; return; }
    if (ev.type !== 'message.created') return;
    const p = (ev.payload ?? {}) as Record<string, unknown>;
    const msg = (p.message ?? p) as Record<string, unknown>;
    connected = true;
    void pushAndScroll({
      id: String(p.id ?? msg.id ?? crypto.randomUUID()),
      platform: String(p.platform ?? 'twitch'),
      username: String(msg.username ?? 'unknown'),
      content: String(msg.content ?? ''),
      isMod: Boolean(msg.is_moderator),
      isSub: Boolean(msg.is_subscriber),
      isVip: Boolean(msg.is_vip),
      ts: Date.now(),
      state: 'live',
    });
  }

  async function send() {
    const text = draft.trim();
    if (!text) return;
    draft = '';
    try {
      await api.post('/api/v1/chat/send', { platform: target, text });
    } catch (err) {
      const msg = err instanceof ApiException && err.status === 404
        ? 'Senden noch nicht verfuegbar (Backend folgt).'
        : 'Nachricht konnte nicht gesendet werden.';
      toast(msg, 'error');
    }
  }

  async function moderate(m: ChatMsg, action: 'delete' | 'timeout' | 'ban') {
    try {
      await api.post('/api/v1/chat/moderate', {
        platform: m.platform,
        message_id: m.id,
        username: m.username,
        action,
      });
      m.state = action === 'delete' ? 'deleted' : action === 'timeout' ? 'timeout' : 'banned';
      messages = [...messages];
    } catch (err) {
      const msg = err instanceof ApiException && err.status === 404
        ? 'Moderation noch nicht verfuegbar (Backend folgt).'
        : 'Aktion fehlgeschlagen.';
      toast(msg, 'error');
    }
  }

  let unlisten: (() => void) | null = null;
  onMount(() => {
    connected = ws.status === 'open';
    unlisten = ws.on(onWsEvent);
  });
  onDestroy(() => unlisten?.());
</script>

<section class="chat-main" data-screen-label="chat">
  <div class="chatbar">
    <div class="seg" role="group" aria-label="Plattform">
      <button class:on={platform === 'all'} onclick={() => (platform = 'all')}>Alle</button>
      <button class:on={platform === 'twitch'} class:tw={platform === 'twitch'} onclick={() => (platform = 'twitch')}>Twitch</button>
      <button class:on={platform === 'youtube'} onclick={() => (platform = 'youtube')}>YouTube</button>
      <button class:on={platform === 'kick'} onclick={() => (platform = 'kick')}>Kick</button>
    </div>
    <button class="ptoggle" class:on={onlyMentions} onclick={() => (onlyMentions = !onlyMentions)}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4" /><path d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-3.9 7.9" /></svg>
      Nur Erwaehnungen
    </button>
    <button class="ptoggle" class:on={onlyMods} onclick={() => (onlyMods = !onlyMods)}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 5 6v5c0 4.5 3 8 7 9 4-1 7-4.5 7-9V6z" /><path d="m9.5 12 1.8 1.8 3.3-3.6" /></svg>
      Nur Mods
    </button>
    <div class="grow"></div>
    <span class="count"><b>{visible.length}</b> Nachrichten</span>
    <button class="icon-btn" class:on={autoScroll} title="Auto-Scroll" onclick={() => (autoScroll = !autoScroll)}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="5" width="3.5" height="14" rx="1" /><rect x="14.5" y="5" width="3.5" height="14" rx="1" /></svg>
    </button>
  </div>

  <div class="feed-wrap">
    <div class="feed" bind:this={feedEl}>
      {#each visible as m (m.id)}
        <div class="msg" class:mention={m.content.toLowerCase().includes('@' + ownName)} class:to={m.state === 'timeout'} class:banned={m.state === 'banned'} class:deleted={m.state === 'deleted'}>
          <span class="ts">{fmtTime(m.ts)}</span>
          <span class="body">
            <span class="pglyph {m.platform === 'twitch' ? 'tw' : m.platform === 'discord' ? 'dc' : ''}"></span>
            {#if m.isMod}<span class="badge mod">MOD</span>{/if}
            {#if m.isVip}<span class="badge vip">VIP</span>{/if}
            {#if m.isSub}<span class="badge sub">SUB</span>{/if}
            <span class="uname" class:struck={m.state === 'banned'}>{m.username}</span>
            <span class="sep">:</span>
            <span class="txt">{m.content}</span>
            <span class="gone">Nachricht entfernt</span>
            {#if m.state === 'timeout'}<span class="penalty to">Timeout</span>{/if}
            {#if m.state === 'banned'}<span class="penalty ban">Ban</span>{/if}
          </span>
          <div class="modbar">
            <button class="mod-act del" onclick={() => moderate(m, 'delete')} aria-label="Loeschen"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" /></svg></button>
            <button class="mod-act timeout" onclick={() => moderate(m, 'timeout')} aria-label="Timeout"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="13" r="8" /><path d="M12 9v4l2.5 2M9 2.5h6" /></svg></button>
            <button class="mod-act ban" onclick={() => moderate(m, 'ban')} aria-label="Ban"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9" /><path d="M5.6 5.6 18.4 18.4" /></svg></button>
          </div>
        </div>
      {/each}
    </div>

    {#if !connected && messages.length === 0}
      <div class="feed-state show">
        <div class="inner">
          <div class="fs-ic">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z" /><path d="M12 16v5" /><path d="m3 3 18 18" /></svg>
          </div>
          <h3>Keine Plattform verbunden</h3>
          <p>Verbinde Twitch, YouTube oder Kick, um den Live-Chat zu empfangen und zu moderieren.</p>
          <p style="font-size:.8rem"><a href="/integrations" style="color:var(--brand);text-decoration:none;font-weight:600">Alle Integrationen verwalten</a></p>
        </div>
      </div>
    {/if}
  </div>

  <div class="composer">
    <div class="composer-inner">
      <select class="chan-select" bind:value={target} aria-label="Ziel-Plattform">
        <option value="all">An alle</option>
        <option value="twitch">Nur Twitch</option>
        <option value="youtube">Nur YouTube</option>
        <option value="kick">Nur Kick</option>
      </select>
      <input
        class="msg-input"
        type="text"
        placeholder="Als {ownName} schreiben..."
        maxlength="500"
        autocomplete="off"
        bind:value={draft}
        onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); send(); } }}
      />
      <button class="send-btn" onclick={send} aria-label="Senden">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2 11 13M22 2l-7 20-4-9-9-4z" /></svg>
      </button>
    </div>
    <div class="note">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 5 6v5c0 4.5 3 8 7 9 4-1 7-4.5 7-9V6z" /></svg>
      Du schreibst als <b style="color:var(--text-dim);margin:0 3px;font-weight:700">Host</b>. Befehle wie <code style="font-family:var(--mono);color:var(--brand)">!so</code> werden unterstuetzt.
    </div>
  </div>
</section>

<style>
  .chat-main { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .chatbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 0 clamp(18px, 2.4vw, 30px) 14px; flex: none; }
  .chatbar .grow { flex: 1; }
  .chatbar .count { font-family: var(--mono); font-size: 11.5px; color: var(--text-faint); letter-spacing: .02em; white-space: nowrap; }
  .chatbar .count b { color: var(--text-dim); font-weight: 600; }
  .icon-btn { width: 36px; height: 36px; border-radius: 10px; border: 1px solid var(--border); background: var(--panel-2); color: var(--text-dim); display: grid; place-items: center; cursor: pointer; transition: .18s var(--ease); flex: none; }
  .icon-btn svg { width: 16px; height: 16px; }
  .icon-btn:hover { color: var(--text); border-color: var(--border-strong); }
  .icon-btn.on { color: var(--brand); background: color-mix(in srgb, var(--brand) 16%, transparent); border-color: color-mix(in srgb, var(--brand) 40%, transparent); }

  .feed-wrap { position: relative; flex: 1; min-height: 0; margin: 0 clamp(14px, 2vw, 24px); border: 1px solid var(--panel-border); border-radius: var(--radius); overflow: hidden; background: var(--panel-bg); box-shadow: var(--panel-shadow); }
  .feed { position: absolute; inset: 0; overflow-y: auto; padding: 8px 2px 14px; overscroll-behavior: contain; }

  .msg { display: grid; grid-template-columns: 52px 1fr; gap: 2px; align-items: baseline; padding: 6px 16px 6px 6px; position: relative; transition: background .12s; }
  .msg:hover { background: var(--panel-3); }
  .msg .ts { font-family: var(--mono); font-size: 10.5px; color: var(--text-faint); text-align: right; padding-right: 10px; padding-top: 2px; letter-spacing: .02em; user-select: none; }
  .msg .body { min-width: 0; font-size: .92rem; line-height: 1.5; word-break: break-word; color: var(--text); }
  .msg .pglyph { vertical-align: -3px; margin-right: 6px; }
  .msg .badge { vertical-align: 1px; margin-right: 5px; }
  .msg .uname { font-weight: 800; letter-spacing: -.01em; }
  .msg .sep { color: var(--text-faint); font-weight: 600; margin: 0 7px 0 2px; }
  .msg.mention { background: color-mix(in srgb, var(--brand) 9%, transparent); box-shadow: inset 3px 0 0 var(--brand); }
  .msg.to .body { opacity: .55; }
  .msg .uname.struck { text-decoration: line-through; text-decoration-thickness: 2px; opacity: .7; }
  .msg.deleted .txt { display: none; }
  .msg .gone { display: none; font-style: italic; color: var(--text-faint); font-size: .86rem; }
  .msg.deleted .gone { display: inline; }
  .msg .penalty { display: inline-flex; font-size: .62rem; font-weight: 800; letter-spacing: .03em; text-transform: uppercase; padding: 1px 6px; border-radius: 5px; margin-left: 7px; vertical-align: 1px; }
  .msg .penalty.to { color: var(--warn); background: color-mix(in srgb, var(--warn) 16%, transparent); }
  .msg .penalty.ban { color: var(--bad); background: color-mix(in srgb, var(--bad) 16%, transparent); }

  .modbar { position: absolute; top: 2px; right: 8px; display: flex; gap: 4px; padding: 3px; border-radius: 11px; background: var(--panel-bg); border: 1px solid var(--panel-border-strong); box-shadow: 0 10px 26px -12px rgba(0,0,0,.6); opacity: 0; transform: translateY(-3px); pointer-events: none; transition: .16s var(--ease); }
  .msg:hover .modbar { opacity: 1; transform: none; pointer-events: auto; }
  .mod-act { width: 28px; height: 28px; border-radius: 8px; border: 0; background: transparent; cursor: pointer; color: var(--text-dim); display: grid; place-items: center; transition: .14s; }
  .mod-act svg { width: 15px; height: 15px; }
  .mod-act:hover { background: var(--panel-3); color: var(--text); }
  .mod-act.timeout:hover { color: var(--warn); background: color-mix(in srgb, var(--warn) 16%, transparent); }
  .mod-act.ban:hover { color: var(--bad); background: color-mix(in srgb, var(--bad) 16%, transparent); }

  .feed-state { position: absolute; inset: 0; z-index: 5; display: none; place-items: center; text-align: center; padding: 24px; background: color-mix(in srgb, var(--panel-bg) 86%, transparent); -webkit-backdrop-filter: blur(8px); backdrop-filter: blur(8px); }
  .feed-state.show { display: grid; }
  .feed-state .inner { max-width: 420px; display: flex; flex-direction: column; align-items: center; gap: 16px; }
  .fs-ic { width: 62px; height: 62px; border-radius: 18px; display: grid; place-items: center; border: 1px solid var(--panel-border-strong); background: var(--panel-2); }
  .fs-ic svg { width: 28px; height: 28px; color: var(--text-dim); }
  .feed-state h3 { font-size: 1.2rem; font-weight: 800; letter-spacing: -.02em; }
  .feed-state p { color: var(--text-dim); font-size: .92rem; line-height: 1.6; }

  .composer { flex: none; padding: 14px clamp(18px, 2.4vw, 30px) clamp(16px, 2.2vw, 22px); }
  .composer-inner { display: flex; align-items: center; gap: 8px; background: var(--field); border: 1px solid var(--border); border-radius: 16px; padding: 6px; transition: border-color .2s, box-shadow .25s; }
  .composer-inner:focus-within { border-color: var(--brand); box-shadow: 0 0 0 4px var(--brand-glow); }
  .chan-select { background: var(--panel-3); border: 1px solid var(--border); border-radius: 11px; padding: 8px 9px; cursor: pointer; color: var(--text); font: inherit; font-weight: 700; font-size: .84rem; outline: none; }
  .msg-input { flex: 1; min-width: 0; border: 0; background: transparent; outline: none; color: var(--text); font: inherit; font-size: .94rem; padding: .6rem .4rem; }
  .msg-input::placeholder { color: var(--text-faint); }
  .send-btn { width: 40px; height: 40px; border-radius: 12px; border: 0; cursor: pointer; flex: none; display: grid; place-items: center; color: var(--on-accent); background: linear-gradient(105deg, var(--brand), var(--brand-2)); box-shadow: 0 10px 22px -10px var(--brand-glow); transition: .16s var(--ease); }
  .send-btn:hover { transform: translateY(-1px); }
  .send-btn svg { width: 18px; height: 18px; }
  .note { font-size: .76rem; color: var(--text-faint); margin-top: 8px; padding-left: 4px; display: flex; align-items: center; gap: 6px; }
  .note svg { width: 13px; height: 13px; }
</style>
