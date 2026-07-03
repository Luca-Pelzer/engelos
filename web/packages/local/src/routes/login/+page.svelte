<script lang="ts">
  import { auth, api, ApiException, setAuthToken, API_BASE } from '@engelos/shared/lib';
  import { theme, setTheme, toast } from '@engelos/shared/lib';
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';

  let providers = $state<{ twitch: boolean; discord: boolean }>({ twitch: true, discord: false });

  // A fresh cache-busting nonce per page load defeats any 302 a browser cached
  // from an earlier deploy whose redirect_uri differed (the localhost-mismatch
  // bug): the unique query forces the click to reach the server for a current
  // authorize URL instead of replaying a stale Location.
  const nonce = Date.now().toString(36);
  const twitchLoginUrl = `${API_BASE}/api/v1/auth/twitch/login?purpose=user&_=${nonce}`;
  const discordLoginUrl = `${API_BASE}/api/v1/auth/discord/login?purpose=user&_=${nonce}`;

  const ACCENTS = [
    { id: 'magma', name: 'Magma', sw: ['#ff5d73', '#ff9e3d'] },
    { id: 'aurora', name: 'Aurora', sw: ['#1fe3b3', '#34c7ff'] },
    { id: 'violet', name: 'Violet', sw: ['#8b5cff', '#43a6ff'] },
    { id: 'lime', name: 'Lime', sw: ['#b6f23d', '#19d3a2'] },
  ];
  const ACCENT_KEY = 'engelos-accent';
  let accentId = $state('magma');

  function applyAccent(id: string) {
    const a = ACCENTS.find((x) => x.id === id) ?? ACCENTS[0];
    accentId = a.id;
    const root = document.documentElement;
    root.style.setProperty('--brand', a.sw[0]);
    root.style.setProperty('--brand-2', a.sw[1]);
    // Store the [primary, secondary] pair - the SAME format shared/lib/theme.ts
    // and the app.html boot script read. Storing the id string here used to make
    // their JSON.parse throw, silently resetting the dashboard to the teal
    // fallback while login showed the picked accent (two-products bug).
    try { localStorage.setItem(ACCENT_KEY, JSON.stringify(a.sw)); } catch { /* private mode */ }
  }

  let email = $state('');
  let password = $state('');
  let showPw = $state(false);
  let remember = $state(false);
  let loading = $state(false);

  // Inline status banner driven by query params the OAuth callbacks redirect
  // back to /login with. Rendered once on mount; no further reactivity needed.
  type BannerKind = 'denied' | 'bot' | 'error';
  let banner = $state<{ kind: BannerKind; text: string } | null>(null);

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (!email || !password) {
      toast('Email and password are required.', 'error');
      return;
    }
    loading = true;
    try {
      const res = await auth.login({ email, password });
      setAuthToken(res.token);
      toast('Welcome back.', 'success');
      goto('/');
    } catch (err) {
      const msg = err instanceof ApiException
        ? (err.status === 0 ? 'Cannot reach the engelOS daemon. Is it running on :8080?' : err.message)
        : 'Login failed. Check your credentials.';
      toast(msg, 'error', 6000);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    // Read OAuth-callback redirect params once on mount and surface a status
    // banner. Strip the param afterwards so a refresh doesn't replay it.
    try {
      const params = new URLSearchParams(window.location.search);
      const denied = params.get('denied');
      const bot = params.get('bot');
      const loginErr = params.get('login');
      if (denied === 'account') {
        banner = {
          kind: 'denied',
          text: "This account isn't authorized for this dashboard. Ask the operator to add it.",
        };
      } else if (bot === 'linked') {
        banner = {
          kind: 'bot',
          text: 'Bot token linked. Sign in with an operator account to open the dashboard.',
        };
      } else if (loginErr === 'error') {
        banner = { kind: 'error', text: 'Login failed. Please try again.' };
      }
      if (banner && window.history.replaceState) {
        window.history.replaceState({}, '', window.location.pathname);
      }
    } catch { /* SSR / no window */ }

    try {
      const sa = localStorage.getItem(ACCENT_KEY);
      if (sa && ACCENTS.some((a) => a.id === sa)) {
        applyAccent(sa);
      } else if (sa) {
        const pair = JSON.parse(sa) as [string, string];
        const match = ACCENTS.find((a) => a.sw[0] === pair[0]);
        applyAccent(match ? match.id : 'magma');
      } else {
        applyAccent('magma');
      }
    } catch { applyAccent('magma'); }

    api.get<{ twitch: boolean; discord: boolean }>('/api/v1/auth/providers')
      .then((p) => { providers = p; })
      .catch(() => { providers = { twitch: true, discord: false }; });

    const reduce = window.matchMedia('(prefers-reduced-motion:reduce)').matches;

    let onMoveOrbs: ((ev: PointerEvent) => void) | null = null;
    if (window.matchMedia('(min-width:760px)').matches && !reduce) {
      const orbs = Array.from(document.querySelectorAll<HTMLElement>('.orb'));
      onMoveOrbs = (ev: PointerEvent) => {
        const x = ev.clientX / window.innerWidth - 0.5;
        const y = ev.clientY / window.innerHeight - 0.5;
        orbs.forEach((o, i) => {
          const f = (i + 1) * 10;
          o.style.transform = `translate(${x * f}px, ${y * f}px)`;
        });
      };
      window.addEventListener('pointermove', onMoveOrbs);
    }

    return () => {
      if (onMoveOrbs) window.removeEventListener('pointermove', onMoveOrbs);
    };
  });
</script>

<div class="scene" aria-hidden="true">
  <div class="orb a"></div>
  <div class="orb b"></div>
  <div class="orb c"></div>
</div>

<div class="topbar">
  <div class="tb-right">
    <div class="accentpick" role="group" aria-label="Accent color">
      {#each ACCENTS as a (a.id)}
        <button
          type="button"
          class:on={accentId === a.id}
          style="background:linear-gradient(135deg,{a.sw[0]},{a.sw[1]})"
          title={a.name}
          aria-label={a.name}
          onclick={() => applyAccent(a.id)}
        ></button>
      {/each}
    </div>
    <div class="toggle" role="group" aria-label="Theme">
      <button type="button" class:on={$theme === 'dark'} onclick={() => setTheme('dark')} aria-label="Dark theme">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" /></svg>
      </button>
      <button type="button" class:on={$theme === 'light'} onclick={() => setTheme('light')} aria-label="Light theme">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>
      </button>
    </div>
  </div>
</div>

<main class="center">
  <div class="card">
    <div class="card-brand reveal d1">
      <span class="cardmark" aria-hidden="true">
        <svg viewBox="0 0 128 128" fill="none"><defs><linearGradient id="cm" x1="40" y1="30" x2="100" y2="100" gradientUnits="userSpaceOnUse"><stop stop-color="var(--brand)" /><stop offset="1" stop-color="var(--brand-2)" /></linearGradient></defs><rect width="128" height="128" rx="30" fill="url(#cm)" /><path d="M48 40 L94 64 L48 88 Z" fill="#0b0e14" stroke="#0b0e14" stroke-width="12" stroke-linejoin="round" stroke-linecap="round" /></svg>
      </span>
      <span class="wordmark">Engel<span class="lo">OS</span></span>
    </div>
    <h2 class="reveal d2">Operator sign-in</h2>
    <p class="lede reveal d2">Sign in to the EngelOS operator dashboard.</p>

    {#if banner}
      <div class="banner reveal d2 {banner.kind}" role="status" aria-live="polite">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          {#if banner.kind === 'denied'}
            <path d="M12 2 2 19h20L12 2Z" /><path d="M12 9v5M12 17h.01" />
          {:else if banner.kind === 'bot'}
            <path d="M5 12.5 10 17 19 8" />
          {:else}
            <circle cx="12" cy="12" r="9" /><path d="M12 8v5M12 16h.01" />
          {/if}
        </svg>
        <span>{banner.text}</span>
      </div>
    {/if}

    <form onsubmit={handleSubmit} novalidate>
      <div class="field reveal d3">
        <label for="email">Email</label>
        <div class="input">
          <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2.5" /><path d="m3.5 7 8.5 6 8.5-6" /></svg></span>
          <input id="email" type="email" name="email" placeholder="you@yourdomain.com" autocomplete="email" bind:value={email} />
        </div>
      </div>

      <div class="field reveal d4">
        <label for="password">Password</label>
        <div class="input">
          <span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="10" width="16" height="11" rx="2.5" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /></svg></span>
          {#if showPw}
            <input id="password" type="text" name="password" placeholder="passphrase" autocomplete="current-password" bind:value={password} />
          {:else}
            <input id="password" type="password" name="password" placeholder="passphrase" autocomplete="current-password" bind:value={password} />
          {/if}
          <button type="button" class="peek" onclick={() => (showPw = !showPw)} aria-label={showPw ? 'Hide password' : 'Show password'}>
            {#if !showPw}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7Z" /><circle cx="12" cy="12" r="3" /></svg>
            {:else}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3l18 18M10.6 10.7a3 3 0 0 0 4.2 4.2M9.4 5.3A9.6 9.6 0 0 1 12 5c6.4 0 10 7 10 7a17 17 0 0 1-3.1 4M6.1 6.2A17 17 0 0 0 2 12s3.6 7 10 7a9.3 9.3 0 0 0 3-.5" /></svg>
            {/if}
          </button>
        </div>
      </div>

      <div class="row-between reveal d5">
        <label class="check">
          <input type="checkbox" bind:checked={remember} />
          <span class="box"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"><path d="m4 12 5 5L20 6" /></svg></span>
          Keep me signed in
        </label>
      </div>

      <button type="submit" class="btn btn-primary reveal d6" disabled={loading}>
        {loading ? 'Signing in' : 'Sign in'}
        {#if !loading}
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6" /></svg>
        {/if}
      </button>

      {#if providers.twitch || providers.discord}
        <div class="divider reveal d7">or continue with</div>

        <div class="social reveal d7" class:single={(providers.twitch ? 1 : 0) + (providers.discord ? 1 : 0) === 1}>
          {#if providers.twitch}
            <a href={twitchLoginUrl} class="btn btn-social twitch" data-sveltekit-reload>
              <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M4.3 3 3 6.4v12.3h4.2V21h2.3l2.3-2.3h3.4L21 14V3H4.3Zm15 10.3-2.6 2.6h-4.2l-2.3 2.3v-2.3H6.7V4.7h12.6v8.6Z" /><path d="M14.7 7.6h1.7v4.6h-1.7zM10.1 7.6h1.7v4.6h-1.7z" /></svg>
              Twitch
            </a>
          {/if}
          {#if providers.discord}
            <a href={discordLoginUrl} class="btn btn-social discord" data-sveltekit-reload>
              <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M19.6 5.6A17 17 0 0 0 15.4 4.3l-.2.4a13 13 0 0 1 3.7 1.9 15.7 15.7 0 0 0-13.8 0 13 13 0 0 1 3.7-1.9l-.2-.4A17 17 0 0 0 4.4 5.6 18.8 18.8 0 0 0 1.2 18.1a17.2 17.2 0 0 0 5.2 2.6l.6-1a11 11 0 0 1-1.8-.9l.4-.3a12.3 12.3 0 0 0 10.8 0l.4.3a11 11 0 0 1-1.8.9l.6 1a17.2 17.2 0 0 0 5.2-2.6 18.8 18.8 0 0 0-3.2-12.5ZM8.9 15.4c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Zm6.2 0c-1 0-1.9-.9-1.9-2.1s.8-2.1 1.9-2.1 1.9 1 1.9 2.1-.8 2.1-1.9 2.1Z" /></svg>
              Discord
            </a>
          {/if}
        </div>
      {/if}
    </form>

    <p class="legal reveal d8">Operator access only.</p>
  </div>
</main>

<style>
  :global(:root) {
    --brand: #ff5d73;
    --brand-2: #ff9e3d;
    --brand-deep: color-mix(in srgb, var(--brand) 72%, #02110d);
    --brand-glow: color-mix(in srgb, var(--brand) 48%, transparent);
    --twitch: #9146ff;
    --discord: #5865f2;
    --lg-radius: 22px;
    --lg-radius-sm: 13px;
    --lg-ease: cubic-bezier(0.2, 0.7, 0.2, 1);
    --scene:
      radial-gradient(115% 85% at 14% 4%, color-mix(in srgb, var(--brand) 34%, transparent) 0%, transparent 52%),
      radial-gradient(120% 110% at 90% 102%, color-mix(in srgb, var(--brand-2) 32%, transparent) 0%, transparent 54%),
      radial-gradient(70% 70% at 68% 30%, color-mix(in srgb, var(--brand) 18%, transparent) 0%, transparent 60%),
      linear-gradient(160deg, color-mix(in srgb, var(--brand) 11%, #0c0c0d) 0%, #08080a 58%, #060607 100%);
    --grid-line: rgba(255, 255, 255, 0.045);
    --card-bg: rgba(20, 20, 23, 0.64);
    --card-border: rgba(255, 255, 255, 0.12);
    --card-hi: rgba(255, 255, 255, 0.06);
    --text: #f4f5f7;
    --text-dim: #b7bbc6;
    --text-faint: #8a8e99;
    --field: rgba(255, 255, 255, 0.075);
    --field-focus: rgba(255, 255, 255, 0.11);
    --border: rgba(255, 255, 255, 0.18);
    --border-strong: rgba(255, 255, 255, 0.28);
    --provider-bg: rgba(255, 255, 255, 0.045);
    --provider-border: rgba(255, 255, 255, 0.14);
    --field-shadow: none;
    --card-shadow: 0 40px 100px -36px rgba(0, 0, 0, 0.85), 0 0 0 1px var(--card-border);
    --on-accent: #04140f;
    --scrim: #070708;
  }

  :global(:root[data-theme='light']) {
    --scene:
      radial-gradient(120% 90% at 10% 0%, color-mix(in srgb, var(--brand) 38%, transparent) 0%, transparent 48%),
      radial-gradient(130% 120% at 94% 106%, color-mix(in srgb, var(--brand-2) 34%, transparent) 0%, transparent 54%),
      radial-gradient(85% 85% at 72% 22%, color-mix(in srgb, var(--brand) 22%, transparent) 0%, transparent 58%),
      linear-gradient(150deg, #edf1f6 0%, #e3e9f1 55%, #d9e1ec 100%);
    --grid-line: rgba(34, 36, 44, 0.15);
    --card-bg: rgba(255, 255, 255, 0.86);
    --card-border: rgba(255, 255, 255, 0.7);
    --card-hi: rgba(255, 255, 255, 0.9);
    --text: #0b1219;
    --text-dim: #44515d;
    --text-faint: #74828e;
    --field: #ffffff;
    --field-focus: #ffffff;
    --border: rgba(13, 20, 28, 0.18);
    --border-strong: rgba(13, 20, 28, 0.32);
    --provider-bg: #ffffff;
    --provider-border: rgba(13, 20, 28, 0.2);
    --field-shadow: 0 1px 2px rgba(13, 20, 28, 0.05), 0 6px 16px -10px rgba(13, 20, 28, 0.16);
    --card-shadow: 0 44px 110px -40px rgba(4, 30, 40, 0.6), 0 0 0 1px var(--card-border);
    --on-accent: #04140f;
    --scrim: #eef3f9;
  }

  * { box-sizing: border-box; }
  :global(body) { background: var(--scene); color: var(--text); font-family: var(--font-sans); -webkit-font-smoothing: antialiased; min-height: 100vh; overflow-x: hidden; }

  .scene { position: fixed; inset: 0; overflow: hidden; z-index: 0; }
  .scene::before { content: ''; position: absolute; inset: 0; background-image: linear-gradient(var(--grid-line) 1px, transparent 1px), linear-gradient(90deg, var(--grid-line) 1px, transparent 1px); background-size: 52px 52px; -webkit-mask-image: radial-gradient(130% 110% at 50% 30%, #000 0%, transparent 78%); mask-image: radial-gradient(130% 110% at 50% 30%, #000 0%, transparent 78%); }
  .scene::after { content: ''; position: absolute; inset: 0; opacity: 0.035; mix-blend-mode: overlay; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='160'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.9' numOctaves='2'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E"); }
  .orb { position: absolute; border-radius: 50%; filter: blur(64px); pointer-events: none; mix-blend-mode: screen; will-change: transform; }
  .orb.a { width: 44vw; height: 44vw; max-width: 620px; max-height: 620px; left: -12vw; top: -14vw; background: radial-gradient(circle, var(--brand) 0%, transparent 68%); opacity: 0.65; animation: f1 17s var(--lg-ease) infinite; }
  .orb.b { width: 40vw; height: 40vw; max-width: 560px; max-height: 560px; right: -12vw; bottom: 6vh; background: radial-gradient(circle, var(--brand-2) 0%, transparent 68%); opacity: 0.56; animation: f2 21s var(--lg-ease) infinite; }
  .orb.c { width: 26vw; height: 26vw; max-width: 360px; max-height: 360px; right: 30%; top: -8vh; background: radial-gradient(circle, color-mix(in srgb, var(--brand), var(--brand-2)) 0%, transparent 70%); opacity: 0.3; animation: f1 25s var(--lg-ease) infinite reverse; }
  @keyframes f1 { 0%, 100% { transform: translate(0, 0) scale(1); } 50% { transform: translate(3%, 4%) scale(1.07); } }
  @keyframes f2 { 0%, 100% { transform: translate(0, 0) scale(1); } 50% { transform: translate(-4%, -3%) scale(1.09); } }
  :global(:root[data-theme='light']) .orb { mix-blend-mode: soft-light; }
  :global(:root[data-theme='light']) .orb.a { opacity: 0.34; }
  :global(:root[data-theme='light']) .orb.b { opacity: 0.3; }
  :global(:root[data-theme='light']) .orb.c { opacity: 0.22; }

  .topbar { position: fixed; top: 0; left: 0; right: 0; z-index: 5; display: flex; align-items: center; justify-content: flex-end; padding: clamp(1.1rem, 2vw, 1.7rem) clamp(1.1rem, 2.4vw, 2.2rem); }
  .tb-right { display: flex; align-items: center; gap: 12px; }
  .accentpick { display: flex; align-items: center; gap: 8px; background-color: var(--card-bg); border: 1px solid var(--card-border); -webkit-backdrop-filter: blur(14px); backdrop-filter: blur(14px); padding: 8px 11px; border-radius: 999px; }
  .accentpick button { width: 17px; height: 17px; border-radius: 50%; border: 0; cursor: pointer; padding: 0; position: relative; transition: transform 0.18s var(--lg-ease); }
  .accentpick button::after { content: ''; position: absolute; inset: -1px; border-radius: 50%; border: 1px solid rgba(255, 255, 255, 0.3); }
  .accentpick button:hover { transform: scale(1.18); }
  .accentpick button.on { box-shadow: 0 0 0 2px var(--card-bg), 0 0 0 3.5px #fff; transform: scale(1.05); }
  .toggle { display: inline-flex; align-items: center; gap: 0.4rem; background-color: var(--card-bg); border: 1px solid var(--card-border); -webkit-backdrop-filter: blur(14px); backdrop-filter: blur(14px); padding: 0.32rem; border-radius: 999px; transition: border-color 0.3s var(--lg-ease); }
  .toggle button { border: 0; background: transparent; cursor: pointer; width: 32px; height: 32px; border-radius: 999px; display: grid; place-items: center; color: var(--text-faint); transition: 0.25s var(--lg-ease); }
  .toggle button.on { background: var(--brand); color: var(--on-accent); box-shadow: 0 6px 16px -6px var(--brand-glow); }
  .toggle svg { width: 16px; height: 16px; }

  .center { position: relative; z-index: 3; min-height: 100svh; display: grid; place-items: center; padding: clamp(5rem, 10vh, 7rem) 1.2rem clamp(2.5rem, 6vh, 4rem); }
  .card { width: 100%; max-width: 440px; position: relative; background-color: var(--card-bg); -webkit-backdrop-filter: blur(30px) saturate(150%); backdrop-filter: blur(30px) saturate(150%); border: 1px solid var(--card-border); border-radius: var(--lg-radius); box-shadow: var(--card-shadow); padding: clamp(1.9rem, 3vw, 2.6rem); transition: box-shadow 0.45s var(--lg-ease); }
  .card::before { content: ''; position: absolute; inset: 0; border-radius: inherit; pointer-events: none; background: linear-gradient(180deg, var(--card-hi), transparent 22%); opacity: 0.5; }
  .card > * { position: relative; }

  .card-brand { display: flex; align-items: center; justify-content: center; gap: 0.6rem; margin-bottom: 1.05rem; }
  .cardmark { width: 38px; height: 38px; flex: none; display: grid; place-items: center; filter: drop-shadow(0 6px 16px var(--brand-glow)); }
  .cardmark svg { width: 38px; height: 38px; display: block; }
  .wordmark { display: inline-flex; align-items: center; gap: 0.06rem; font-weight: 800; font-size: 1.55rem; letter-spacing: -0.03em; line-height: 1; }
  .wordmark .lo { color: var(--brand); }
  .card h2 { font-size: 1.55rem; font-weight: 800; letter-spacing: -0.03em; text-align: center; }
  .card .lede { margin-top: 0.5rem; color: var(--text-dim); font-size: 0.95rem; text-align: center; }

  .banner { display: flex; align-items: flex-start; gap: 0.6rem; margin-top: 1.1rem; padding: 0.8rem 0.95rem; border-radius: var(--lg-radius-sm); font-size: 0.86rem; line-height: 1.4; border: 1px solid var(--border); background: var(--field); color: var(--text-dim); }
  .banner svg { width: 18px; height: 18px; flex: none; margin-top: 0.05rem; }
  .banner.denied { border-color: color-mix(in srgb, #ff4d5e 55%, transparent); background: color-mix(in srgb, #ff4d5e 12%, var(--field)); color: var(--text); }
  .banner.denied svg { color: #ff4d5e; }
  .banner.bot { border-color: color-mix(in srgb, var(--brand) 45%, transparent); background: color-mix(in srgb, var(--brand) 10%, var(--field)); }
  .banner.bot svg { color: var(--brand); }
  .banner.error { border-color: var(--border-strong); }
  .banner.error svg { color: var(--text-dim); }

  form { margin-top: 1.7rem; display: flex; flex-direction: column; gap: 1rem; }
  .field label { display: block; font-size: 0.8rem; font-weight: 600; color: var(--text-dim); margin-bottom: 0.45rem; letter-spacing: 0.01em; }
  .input { position: relative; display: flex; align-items: center; background-color: var(--field); border: 1px solid var(--border); border-radius: var(--lg-radius-sm); box-shadow: var(--field-shadow); transition: border-color 0.2s, box-shadow 0.25s; }
  .input:focus-within { border-color: var(--brand); background-color: var(--field-focus); box-shadow: 0 0 0 4px var(--brand-glow), 0 0 26px -8px var(--brand-glow); }
  .input .lead { display: grid; place-items: center; width: 46px; color: var(--text-faint); flex: none; }
  .input .lead svg { width: 18px; height: 18px; }
  .input input { flex: 1; border: 0; background: transparent; outline: none; color: var(--text); font: inherit; font-size: 0.96rem; padding: 0.9rem 0.5rem 0.9rem 0; }
  .input input::placeholder { color: var(--text-faint); }
  .input .peek { border: 0; background: transparent; cursor: pointer; color: var(--text-faint); width: 46px; align-self: stretch; display: grid; place-items: center; transition: color 0.2s; }
  .input .peek:hover { color: var(--text); }
  .input .peek svg { width: 18px; height: 18px; }

  .row-between { display: flex; align-items: center; justify-content: space-between; margin-top: -0.1rem; }
  .check { display: flex; align-items: center; gap: 0.5rem; cursor: pointer; font-size: 0.86rem; color: var(--text-dim); user-select: none; }
  .check input { position: absolute; opacity: 0; width: 0; height: 0; }
  .box { width: 18px; height: 18px; border-radius: 6px; border: 1.5px solid var(--border-strong); display: grid; place-items: center; transition: 0.2s var(--lg-ease); }
  .box svg { width: 11px; height: 11px; opacity: 0; transform: scale(0.6); transition: 0.2s var(--lg-ease); color: var(--on-accent); }
  .check input:checked + .box { background: var(--brand); border-color: var(--brand); }
  .check input:checked + .box svg { opacity: 1; transform: scale(1); }
  .check input:focus-visible + .box { box-shadow: 0 0 0 4px var(--brand-glow); }

  .btn { border: 0; cursor: pointer; font: inherit; font-weight: 700; font-size: 0.98rem; border-radius: var(--lg-radius-sm); padding: 1rem 1.2rem; display: flex; align-items: center; justify-content: center; gap: 0.6rem; transition: transform 0.15s var(--lg-ease), box-shadow 0.25s, filter 0.2s; letter-spacing: 0.01em; text-decoration: none; }
  .btn:active { transform: translateY(1px) scale(0.995); }
  .btn-primary { color: var(--on-accent); position: relative; overflow: hidden; margin-top: 0.3rem; background: linear-gradient(105deg, var(--brand), var(--brand-2)); box-shadow: 0 14px 32px -12px var(--brand-glow), 0 0 0 1px rgba(255, 255, 255, 0.14) inset; }
  .btn-primary:hover { transform: translateY(-2px); box-shadow: 0 20px 48px -14px var(--brand-glow), 0 0 0 1px rgba(255, 255, 255, 0.2) inset; }
  .btn-primary:disabled { opacity: 0.7; cursor: not-allowed; transform: none; }
  .btn-primary::after { content: ''; position: absolute; top: 0; left: -120%; width: 60%; height: 100%; background: linear-gradient(100deg, transparent, rgba(255, 255, 255, 0.45), transparent); transform: skewX(-18deg); transition: left 0.6s var(--lg-ease); }
  .btn-primary:hover::after { left: 140%; }
  .btn-primary svg { width: 18px; height: 18px; }

  .divider { display: flex; align-items: center; gap: 1rem; color: var(--text-faint); font-size: 0.78rem; letter-spacing: 0.02em; margin: 0.3rem 0; }
  .divider::before, .divider::after { content: ''; flex: 1; height: 1px; background: var(--border); }

  .social { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
  .social.single { grid-template-columns: 1fr; }
  .btn-social { background: var(--provider-bg); border: 1px solid var(--provider-border); color: var(--text); font-size: 0.9rem; font-weight: 600; position: relative; overflow: hidden; }
  .btn-social svg { width: 19px; height: 19px; flex: none; }
  .btn-social::before { content: ''; position: absolute; inset: 0; opacity: 0; transition: opacity 0.25s; background: radial-gradient(120% 140% at 50% 120%, var(--c) 0%, transparent 70%); }
  .btn-social:hover { transform: translateY(-2px); border-color: var(--c); box-shadow: 0 14px 30px -14px var(--c); }
  .btn-social:hover::before { opacity: 0.16; }
  .btn-social.twitch { --c: var(--twitch); }
  .btn-social.twitch svg { color: var(--twitch); }
  .btn-social.discord { --c: var(--discord); }
  .btn-social.discord svg { color: var(--discord); }

  .legal { margin-top: 1.5rem; text-align: center; font-size: 0.78rem; color: var(--text-faint); line-height: 1.6; }

  @keyframes up { from { opacity: 0; transform: translateY(16px); } to { opacity: 1; transform: translateY(0); } }
  .reveal { opacity: 0; animation: up 0.7s var(--lg-ease) forwards; }
  .d1 { animation-delay: 0.04s; } .d2 { animation-delay: 0.1s; } .d3 { animation-delay: 0.16s; } .d4 { animation-delay: 0.22s; }
  .d5 { animation-delay: 0.28s; } .d6 { animation-delay: 0.34s; } .d7 { animation-delay: 0.4s; } .d8 { animation-delay: 0.46s; }

  @media (max-width: 520px) {
    .social { grid-template-columns: 1fr; }
    .card { padding: 1.5rem 1.25rem; }
  }
  @media (prefers-reduced-motion: reduce) {
    .reveal { opacity: 1; animation: none; }
    .orb { animation: none; }
  }
</style>
