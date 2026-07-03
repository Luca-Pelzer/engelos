<script lang="ts">
  import { api, API_BASE, toast, activeWorkspace } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  async function copyAvatarOverlayURL() {
    const channel = $activeWorkspace;
    if (!channel) { toast('Select a workspace first.', 'warn'); return; }
    try {
      const t = await api.get<{ channel: string; token: string }>(
        `/api/v1/overlay/avatar/token?channel=${encodeURIComponent(channel)}`,
      );
      const url = `${window.location.origin}/overlay/avatar?channel=${encodeURIComponent(t.channel)}&token=${encodeURIComponent(t.token)}`;
      await navigator.clipboard.writeText(url);
      toast('Overlay URL copied - paste it into an OBS browser source.', 'success');
    } catch {
      toast('Could not fetch the overlay URL.', 'error');
    }
  }

  type Conn = { id: string; provider: string };

  type Integration = {
    id: string;
    name: string;
    color: string;
    connectHref: string | null;
    acct: string;
    desc: string;
    feats: string[];
    on: boolean;
  };

  const base: Omit<Integration, 'on'>[] = [
    { id: 'twitch', name: 'Twitch', color: 'var(--twitch)', connectHref: `${API_BASE}/api/v1/auth/twitch/login?purpose=bot`, acct: '', desc: 'Receive and moderate your Twitch chat, sync subs, follows and bits in real time.', feats: ['Live-Chat', 'Mod actions', 'Sub-Alerts', 'Channel-Points'] },
    { id: 'discord', name: 'Discord', color: 'var(--discord)', connectHref: `${API_BASE}/api/v1/auth/discord/login?purpose=bot`, acct: '', desc: 'Post go-live announcements and event notifications to your server.', feats: ['Go-live ping', 'Event archive', 'Auto roles'] },
    { id: 'spotify', name: 'Spotify', color: 'var(--spotify)', connectHref: `${API_BASE}/api/v1/auth/spotify/login`, acct: '', desc: 'Show the current song as an overlay and let viewers follow along via !song.', feats: ['Now-Playing', '!song command', 'Overlay'] },
  ];

  const soon = [
    { id: 'kick', name: 'Kick', color: 'var(--kick)', desc: 'Receive Kick chat and moderate it together with Twitch and Discord.', feats: ['Live-Chat', 'Multistream'] },
    { id: 'youtube', name: 'YouTube Live', color: 'var(--youtube)', desc: 'Bring in and moderate YouTube live chat alongside your other platforms.', feats: ['Live-Chat', 'Super-Chats'] },
  ];

  // Workflow integrations come from the backend registry: every registered
  // integration contributes its manifest (card) and its workflow nodes.
  type RegistryIntegration = {
    id: string;
    name: string;
    description: string;
    icon: string;
    auth_kind: string;
    setup_href: string;
    connected: boolean;
    credential_keys: string[];
  };
  let registry = $state<RegistryIntegration[]>([]);

  // Toggleable plugins from the plugin boundary (GET /api/v1/plugins).
  // Toggles persist immediately and take effect on the next daemon restart.
  type PluginEntry = {
    id: string;
    name: string;
    description: string;
    tier: string;
    settings_href?: string;
    default_enabled: boolean;
    enabled: boolean;
    desired: boolean;
    restart_required: boolean;
  };
  let pluginList = $state<PluginEntry[]>([]);
  let toggling = $state<string | null>(null);

  async function togglePlugin(p: PluginEntry) {
    if (toggling) return;
    toggling = p.id;
    try {
      const u = await api.put<PluginEntry>(`/api/v1/plugins/${encodeURIComponent(p.id)}`, { enabled: !p.desired });
      pluginList = pluginList.map((x) => (x.id === p.id ? u : x));
      if (u.restart_required) toast('Saved. Takes effect after the next bot restart.', 'success');
      else toast(u.desired ? 'Plugin enabled.' : 'Plugin disabled.', 'success');
    } catch {
      toast('Could not update the plugin.', 'error');
    } finally {
      toggling = null;
    }
  }

  // Active bundled plugins, re-homed here after the primary-nav collapse. These
  // are real, supported surfaces — they just no longer warrant their own rail
  // icon. Each links straight to its existing route.
  type Plugin = { id: string; name: string; href: string; color: string; desc: string; icon: string };
  const plugins: Plugin[] = [
    { id: 'tts', name: 'AI Voice', href: '/tts', color: 'var(--brand)', desc: 'Text-to-speech plugin: read chat messages, redemptions or alerts aloud with configurable voices.', icon: '<path d="M12 2a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>' },
    { id: 'translate', name: 'Translate', href: '/translate', color: 'var(--brand-2)', desc: 'Translation plugin: mirror chat messages into other languages for a multilingual audience.', icon: '<path d="M4 5h7M9 3v2c0 4-2 7-5 8M5 9c0 3 3 5 6 6M13 19l4-9 4 9M14.5 16h5"/>' },
    { id: 'clipper', name: 'Clip Workflow', href: '/clipper', color: 'var(--twitch)', desc: 'Clip plugin built on Twitch.CreateClip: create and manage clips as a workflow action.', icon: '<path d="M6 4v16M18 4v16M6 8h12M6 16h12M2 8h4M18 8h4"/>' },
    { id: 'cohost', name: 'AI Co-Host', href: '/cohost', color: 'var(--text-dim)', desc: 'Experimental adapter seed for an AI co-host. Not a core feature.', icon: '<path d="M12 3a4 4 0 0 1 4 4v1a4 4 0 0 1 2 7l-1 .5V17a3 3 0 0 1-3 3h-4a3 3 0 0 1-3-3v-1.5L6 15a4 4 0 0 1 2-7V7a4 4 0 0 1 4-4z"/>' },
  ];

  let integrations = $state<Integration[]>(base.map((b) => ({ ...b, on: false })));

  const connectedCount = $derived(integrations.filter((i) => i.on).length);

  const tierLabel: Record<string, string> = {
    legacy: 'Legacy',
    template: 'Template',
    experimental: 'Experimental',
    'core-adjacent': 'Core-adjacent',
  };

  const logos: Record<string, string> = {
    twitch: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M4.3 3 3 6.4v12.3h4.2V21h2.3l2.3-2.3h3.4L21 14V3H4.3Zm15 10.3-2.6 2.6h-4.2l-2.3 2.3v-2.3H6.7V4.7h12.6v8.6Z"/></svg>',
    discord: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19.6 5.6A17 17 0 0 0 15.4 4.3l-.2.4a13 13 0 0 1 3.7 1.9 15.7 15.7 0 0 0-13.8 0 13 13 0 0 1 3.7-1.9l-.2-.4A17 17 0 0 0 4.4 5.6 18.8 18.8 0 0 0 1.2 18.1a17.2 17.2 0 0 0 5.2 2.6l.6-1a11 11 0 0 1-1.8-.9l.4-.3a12.3 12.3 0 0 0 10.8 0l.4.3a11 11 0 0 1-1.8.9l.6 1a17.2 17.2 0 0 0 5.2-2.6 18.8 18.8 0 0 0-3.2-12.5ZM8.9 15.4c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Zm6.2 0c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Z"/></svg>',
    spotify: '<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="12" r="10"/><path d="M7 9.6c3.2-.9 6.6-.6 9.2 1M7.6 13c2.6-.7 5.3-.4 7.4.9M8 16c2-.5 4-.3 5.6.7" stroke="#000" stroke-width="1.5" stroke-linecap="round" fill="none"/></svg>',
    kick: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M4 3h4v6l5-6h5l-6 8 6 10h-5l-5-7v7H4z"/></svg>',
    youtube: '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="2" y="5" width="20" height="14" rx="4.5"/><path d="M10 8.8 15.5 12 10 15.2z" fill="#fff"/></svg>',
    obs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><circle cx="9.4" cy="13.6" r="3.4"/></svg>',
    elevenlabs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/></svg>',
    'ai-backend': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a4 4 0 0 1 4 4v1a4 4 0 0 1 2 7l-1 .5V17a3 3 0 0 1-3 3h-4a3 3 0 0 1-3-3v-1.5L6 15a4 4 0 0 1 2-7V7a4 4 0 0 1 4-4z"/><path d="M9 12h6M12 9v6"/></svg>',
  };

  const registryColors: Record<string, string> = {
    elevenlabs: 'var(--brand)',
    obs: 'var(--obs)',
    'ai-backend': 'var(--brand-2)',
  };

  onMount(async () => {
    try {
      const conns = await api.get<Conn[]>('/api/v1/connections');
      const byProvider = new Set(conns.map((c) => c.provider));
      integrations = base.map((b) => ({ ...b, on: byProvider.has(b.id) }));
    } catch {
      integrations = base.map((b) => ({ ...b, on: false }));
    }
    try {
      registry = await api.get<RegistryIntegration[]>('/api/v1/integrations');
    } catch {
      registry = [];
    }
    try {
      pluginList = await api.get<PluginEntry[]>('/api/v1/plugins');
    } catch {
      pluginList = [];
    }
  });
</script>

<section class="int-scroll" data-screen-label="integrations">
  <div class="int-summary">
    <span><b>{connectedCount}</b> of <b>{integrations.length}</b> available services connected</span>
    <span class="bar"><i style="width:{(connectedCount / integrations.length) * 100}%"></i></span>
  </div>

  <div class="sect-label">Platforms</div>
  <div class="int-grid">
    {#each integrations as it (it.id)}
      <div class="int-card" style="--c:{it.color}">
        <div class="int-top">
          <span class="int-logo">{@html logos[it.id]}</span>
          <div class="int-titles">
            <h3>{it.name}</h3>
            {#if it.on}<span class="int-status on">Connected</span>{:else}<span class="int-status off">Not connected</span>{/if}
          </div>
        </div>
        <p class="int-desc">{it.desc}</p>
        <div class="int-feats">
          {#each it.feats as f}<span class="int-feat">{f}</span>{/each}
        </div>
        <div class="int-foot">
          {#if it.on}
            <a href="/connections" class="int-btn disconnect">Manage</a>
          {:else if it.connectHref}
            <a href={it.connectHref} class="int-btn connect" data-sveltekit-reload>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z" /><path d="M12 16v5" /></svg>
              Connect
            </a>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  {#if registry.length}
    <div class="sect-label">Integrations</div>
    <div class="int-grid">
      {#each registry as it (it.id)}
        <div class="int-card" style="--c:{registryColors[it.id] ?? 'var(--brand)'}">
          <div class="int-top">
            <span class="int-logo">{@html logos[it.id] ?? logos['obs']}</span>
            <div class="int-titles">
              <h3>{it.name}</h3>
              {#if it.connected}<span class="int-status on">Connected</span>{:else}<span class="int-status off">Not configured</span>{/if}
            </div>
          </div>
          <p class="int-desc">{it.description}</p>
          <div class="int-feats">
            <span class="int-feat">{it.auth_kind === 'apikey' ? 'API key' : it.auth_kind === 'oauth' ? 'OAuth' : 'No auth'}</span>
            <span class="int-feat">Workflow nodes</span>
          </div>
          <div class="int-foot">
            {#if it.id === 'avatar'}
              <button class="int-btn connect" onclick={copyAvatarOverlayURL}>Copy overlay URL</button>
            {:else if it.setup_href}
              <a href={it.setup_href} class="int-btn {it.connected ? 'disconnect' : 'connect'}" data-sveltekit-preload-data="hover">
                {it.connected ? 'Manage' : 'Set up'}
              </a>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <div class="sect-label">Plugins</div>
  <div class="int-grid">
    {#each plugins as p (p.id)}
      <a class="int-card plugin" href={p.href} style="--c:{p.color}" data-sveltekit-preload-data="hover">
        <div class="int-top">
          <span class="int-logo">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">{@html p.icon}</svg>
          </span>
          <div class="int-titles">
            <h3>{p.name}</h3>
            <span class="int-status plugin">Plugin</span>
          </div>
        </div>
        <p class="int-desc">{p.desc}</p>
        <div class="int-foot">
          <span class="int-btn open">
            Open
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6" /></svg>
          </span>
        </div>
      </a>
    {/each}
  </div>

  <div class="sect-label">Coming soon</div>
  <div class="int-grid">
    {#each soon as it (it.id)}
      <div class="int-card soon" style="--c:{it.color}">
        <div class="int-top">
          <span class="int-logo">{@html logos[it.id]}</span>
          <div class="int-titles">
            <h3>{it.name}</h3>
            <span class="int-status soon">Available soon</span>
          </div>
        </div>
        <p class="int-desc">{it.desc}</p>
        <div class="int-feats">
          {#each it.feats as f}<span class="int-feat">{f}</span>{/each}
        </div>
        <div class="int-foot">
          <button class="int-btn disconnect" disabled>Benachrichtigen</button>
        </div>
      </div>
    {/each}
  </div>

  {#if pluginList.length}
    <div class="sect-label">Toggleable plugins</div>
    <div class="plugin-list">
      {#each pluginList as p (p.id)}
        <div class="plugin-row" class:off={!p.desired}>
          <div class="plugin-main">
            <span class="plugin-name">{p.name}</span>
            <span class="legacy-badge">{tierLabel[p.tier] ?? p.tier}</span>
            {#if p.restart_required}<span class="restart-pill">Restart required</span>{/if}
          </div>
          <span class="plugin-desc">{p.description}</span>
          <div class="plugin-side">
            {#if p.settings_href && p.enabled}
              <a class="int-btn disconnect sm" href={p.settings_href} data-sveltekit-preload-data="hover">Open</a>
            {/if}
            <button
              class="switch" class:on={p.desired}
              onclick={() => togglePlugin(p)}
              disabled={toggling === p.id}
              aria-label={`Toggle ${p.name}`}
            ></button>
          </div>
        </div>
      {/each}
    </div>
    <p class="plugin-note">Toggles save immediately and take effect after the next bot restart.</p>
  {/if}
</section>

<style>
  .int-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 4px clamp(18px, 3vw, 40px) 56px; }
  .int-summary { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; margin: 6px 0 22px; color: var(--text-dim); font-size: .92rem; }
  .int-summary .bar { flex: 1; min-width: 160px; max-width: 280px; height: 8px; border-radius: 99px; background: var(--panel-2); overflow: hidden; border: 1px solid var(--panel-border); }
  .int-summary .bar i { display: block; height: 100%; border-radius: 99px; background: linear-gradient(90deg, var(--brand), var(--brand-2)); transition: width .5s var(--ease); }
  .int-summary b { color: var(--text); font-weight: 700; }
  .sect-label { font-size: .72rem; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; color: var(--text-faint); margin: 26px 0 14px; }
  .int-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(310px, 1fr)); gap: 16px; }
  .int-card { position: relative; display: flex; flex-direction: column; padding: 20px 20px 18px; border-radius: var(--radius); overflow: hidden; background: var(--panel-bg); border: 1px solid var(--panel-border); box-shadow: var(--panel-shadow); transition: transform .2s var(--ease), border-color .2s; }
  .int-card::before { content: ""; position: absolute; inset: 0; border-radius: inherit; pointer-events: none; background: linear-gradient(180deg, var(--hi), transparent 22%); opacity: .5; }
  .int-card::after { content: ""; position: absolute; top: 0; left: 0; right: 0; height: 3px; background: var(--c); opacity: .9; }
  .int-card:hover { transform: translateY(-3px); border-color: var(--border-strong); }
  .int-card.soon { opacity: .72; }
  .int-card > * { position: relative; }
  .int-top { display: flex; align-items: flex-start; gap: 13px; margin-bottom: 14px; }
  .int-logo { width: 48px; height: 48px; border-radius: 14px; flex: none; display: grid; place-items: center; color: #fff; background: var(--c); box-shadow: 0 10px 24px -10px var(--c); }
  .int-logo :global(svg) { width: 26px; height: 26px; }
  .int-titles { flex: 1; min-width: 0; }
  .int-titles h3 { font-size: 1.08rem; font-weight: 800; letter-spacing: -.02em; }
  .int-status { display: inline-flex; align-items: center; gap: 6px; font-size: .74rem; font-weight: 700; margin-top: 5px; padding: 3px 9px; border-radius: 99px; letter-spacing: .02em; }
  .int-status::before { content: ""; width: 7px; height: 7px; border-radius: 50%; }
  .int-status.on { color: var(--ok); background: color-mix(in srgb, var(--ok) 14%, transparent); }
  .int-status.on::before { background: var(--ok); box-shadow: 0 0 8px var(--ok); }
  .int-status.off { color: var(--text-faint); background: var(--panel-3); }
  .int-status.off::before { background: var(--text-faint); }
  .int-status.soon { color: var(--warn); background: color-mix(in srgb, var(--warn) 14%, transparent); }
  .int-status.soon::before { background: var(--warn); }
  .int-desc { color: var(--text-dim); font-size: .88rem; line-height: 1.55; flex: 1; }
  .int-feats { display: flex; flex-wrap: wrap; gap: 6px; margin: 14px 0 16px; }
  .int-feat { font-size: .72rem; font-weight: 600; color: var(--text-dim); background: var(--panel-2); border: 1px solid var(--panel-border); padding: 4px 9px; border-radius: 7px; }
  .int-foot { display: flex; align-items: center; gap: 10px; margin-top: auto; }
  .int-btn { flex: none; font-weight: 700; font-size: .85rem; padding: .6rem 1.05rem; border-radius: 11px; cursor: pointer; border: 1px solid; transition: .18s var(--ease); display: inline-flex; align-items: center; gap: .45rem; text-decoration: none; }
  .int-btn svg { width: 15px; height: 15px; }
  .int-btn.connect { color: #fff; background: var(--c); border-color: transparent; }
  .int-btn.connect:hover { filter: brightness(1.08); transform: translateY(-1px); }
  .int-btn.disconnect { color: var(--text-dim); background: transparent; border-color: var(--border); }
  .int-btn.disconnect:hover { color: var(--text); border-color: var(--border-strong); }
  .int-btn:disabled { cursor: not-allowed; opacity: .6; }

  /* Plugin cards: bundled, supported surfaces re-homed from the rail. The whole
     card is a link; reuse the int-card shell with a plain "open" affordance. */
  .int-card.plugin { text-decoration: none; cursor: pointer; }
  .int-status.plugin { color: var(--brand); background: color-mix(in srgb, var(--brand) 14%, transparent); }
  .int-status.plugin::before { background: var(--brand); box-shadow: 0 0 8px var(--brand-glow); }
  .int-btn.open { color: var(--text-dim); background: transparent; border-color: var(--border); }
  .int-card.plugin:hover .int-btn.open { color: var(--text); border-color: var(--border-strong); }

  /* Toggleable plugins (plugin boundary, /api/v1/plugins) */
  .plugin-list { display: flex; flex-direction: column; gap: 8px; }
  .plugin-row { display: grid; grid-template-columns: minmax(200px, 260px) 1fr auto; align-items: center; gap: 14px; padding: 13px 16px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); transition: border-color .18s var(--ease), opacity .18s var(--ease); }
  .plugin-row:hover { border-color: var(--border-strong); }
  .plugin-row.off { opacity: .6; }
  .plugin-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .plugin-name { font-size: .95rem; font-weight: 700; color: var(--text); }
  .legacy-badge { font-size: .66rem; font-weight: 800; letter-spacing: .05em; text-transform: uppercase; color: var(--text-faint); background: var(--panel-2); border: 1px solid var(--panel-border); padding: 2px 7px; border-radius: 6px; }
  .restart-pill { font-size: .68rem; font-weight: 700; color: var(--warn); background: color-mix(in srgb, var(--warn) 14%, transparent); padding: 2px 8px; border-radius: 999px; white-space: nowrap; }
  .plugin-desc { font-size: .82rem; color: var(--text-dim); line-height: 1.5; min-width: 0; }
  .plugin-side { display: flex; align-items: center; gap: 10px; }
  .int-btn.sm { padding: .4rem .8rem; font-size: .78rem; }
  .plugin-note { margin: 10px 2px 0; font-size: .78rem; color: var(--text-faint); }
  @media (max-width: 760px) { .plugin-row { grid-template-columns: 1fr auto; } .plugin-desc { grid-column: 1 / -1; } }
</style>
