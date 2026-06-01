<script lang="ts">
  import { api, API_BASE } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

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
    { id: 'twitch', name: 'Twitch', color: 'var(--twitch)', connectHref: `${API_BASE}/api/v1/auth/twitch/login?purpose=bot`, acct: '', desc: 'Empfange und moderiere deinen Twitch-Chat, synchronisiere Subs, Follows und Bits in Echtzeit.', feats: ['Live-Chat', 'Mod-Aktionen', 'Sub-Alerts', 'Channel-Points'] },
    { id: 'discord', name: 'Discord', color: 'var(--discord)', connectHref: `${API_BASE}/api/v1/auth/discord/login?purpose=bot`, acct: '', desc: 'Poste Go-Live-Ankuendigungen und Event-Benachrichtigungen in deinen Server.', feats: ['Go-Live-Ping', 'Event-Archiv', 'Auto-Rollen'] },
    { id: 'spotify', name: 'Spotify', color: 'var(--spotify)', connectHref: `${API_BASE}/api/v1/auth/spotify/login`, acct: '', desc: 'Zeige den aktuellen Song als Overlay an und lass Zuschauer per !song mitlesen.', feats: ['Now-Playing', '!song Befehl', 'Overlay'] },
  ];

  const soon = [
    { id: 'kick', name: 'Kick', color: 'var(--kick)', desc: 'Kick-Chat empfangen und gemeinsam mit Twitch und Discord moderieren.', feats: ['Live-Chat', 'Multistream'] },
    { id: 'youtube', name: 'YouTube Live', color: 'var(--youtube)', desc: 'Live-Chat von YouTube-Streams parallel einbinden und moderieren.', feats: ['Live-Chat', 'Super-Chats'] },
    { id: 'obs', name: 'OBS Studio', color: 'var(--obs)', desc: 'Steuere Szenen und triggere Alerts direkt aus EngelOS.', feats: ['Szenen-Wechsel', 'Alerts'] },
  ];

  let integrations = $state<Integration[]>(base.map((b) => ({ ...b, on: false })));

  const connectedCount = $derived(integrations.filter((i) => i.on).length);

  const logos: Record<string, string> = {
    twitch: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M4.3 3 3 6.4v12.3h4.2V21h2.3l2.3-2.3h3.4L21 14V3H4.3Zm15 10.3-2.6 2.6h-4.2l-2.3 2.3v-2.3H6.7V4.7h12.6v8.6Z"/></svg>',
    discord: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19.6 5.6A17 17 0 0 0 15.4 4.3l-.2.4a13 13 0 0 1 3.7 1.9 15.7 15.7 0 0 0-13.8 0 13 13 0 0 1 3.7-1.9l-.2-.4A17 17 0 0 0 4.4 5.6 18.8 18.8 0 0 0 1.2 18.1a17.2 17.2 0 0 0 5.2 2.6l.6-1a11 11 0 0 1-1.8-.9l.4-.3a12.3 12.3 0 0 0 10.8 0l.4.3a11 11 0 0 1-1.8.9l.6 1a17.2 17.2 0 0 0 5.2-2.6 18.8 18.8 0 0 0-3.2-12.5ZM8.9 15.4c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Zm6.2 0c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Z"/></svg>',
    spotify: '<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="12" r="10"/><path d="M7 9.6c3.2-.9 6.6-.6 9.2 1M7.6 13c2.6-.7 5.3-.4 7.4.9M8 16c2-.5 4-.3 5.6.7" stroke="#000" stroke-width="1.5" stroke-linecap="round" fill="none"/></svg>',
    kick: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M4 3h4v6l5-6h5l-6 8 6 10h-5l-5-7v7H4z"/></svg>',
    youtube: '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="2" y="5" width="20" height="14" rx="4.5"/><path d="M10 8.8 15.5 12 10 15.2z" fill="#fff"/></svg>',
    obs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><circle cx="9.4" cy="13.6" r="3.4"/></svg>',
  };

  onMount(async () => {
    try {
      const conns = await api.get<Conn[]>('/api/v1/connections');
      const byProvider = new Set(conns.map((c) => c.provider));
      integrations = base.map((b) => ({ ...b, on: byProvider.has(b.id) }));
    } catch {
      integrations = base.map((b) => ({ ...b, on: false }));
    }
  });
</script>

<section class="int-scroll" data-screen-label="integrations">
  <div class="int-summary">
    <span><b>{connectedCount}</b> von <b>{integrations.length}</b> verfuegbaren Diensten verbunden</span>
    <span class="bar"><i style="width:{(connectedCount / integrations.length) * 100}%"></i></span>
  </div>

  <div class="int-grid">
    {#each integrations as it (it.id)}
      <div class="int-card" style="--c:{it.color}">
        <div class="int-top">
          <span class="int-logo">{@html logos[it.id]}</span>
          <div class="int-titles">
            <h3>{it.name}</h3>
            {#if it.on}<span class="int-status on">Verbunden</span>{:else}<span class="int-status off">Nicht verbunden</span>{/if}
          </div>
        </div>
        <p class="int-desc">{it.desc}</p>
        <div class="int-feats">
          {#each it.feats as f}<span class="int-feat">{f}</span>{/each}
        </div>
        <div class="int-foot">
          {#if it.on}
            <a href="/connections" class="int-btn disconnect">Verwalten</a>
          {:else if it.connectHref}
            <a href={it.connectHref} class="int-btn connect" data-sveltekit-reload>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z" /><path d="M12 16v5" /></svg>
              Verbinden
            </a>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  <div class="sect-label">Demnaechst verfuegbar</div>
  <div class="int-grid">
    {#each soon as it (it.id)}
      <div class="int-card soon" style="--c:{it.color}">
        <div class="int-top">
          <span class="int-logo">{@html logos[it.id]}</span>
          <div class="int-titles">
            <h3>{it.name}</h3>
            <span class="int-status soon">Bald verfuegbar</span>
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
</style>
