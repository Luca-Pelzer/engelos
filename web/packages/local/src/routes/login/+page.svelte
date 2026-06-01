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
    try { localStorage.setItem(ACCENT_KEY, a.id); } catch { /* private mode */ }
  }

  let email = $state('');
  let password = $state('');
  let showPw = $state(false);
  let remember = $state(false);
  let loading = $state(false);

  let eqEl: HTMLDivElement;

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
    try {
      const sa = localStorage.getItem(ACCENT_KEY);
      if (sa && ACCENTS.some((a) => a.id === sa)) applyAccent(sa);
      else applyAccent('magma');
    } catch { applyAccent('magma'); }

    api.get<{ twitch: boolean; discord: boolean }>('/api/v1/auth/providers')
      .then((p) => { providers = p; })
      .catch(() => { providers = { twitch: true, discord: false }; });

    const reduce = window.matchMedia('(prefers-reduced-motion:reduce)').matches;

    // Sparklines in the faux dashboard stat cards.
    document.querySelectorAll<HTMLElement>('.dstat .spark').forEach((sp, idx) => {
      let html = '';
      for (let i = 0; i < 12; i++) {
        const h = 24 + Math.round(Math.abs(Math.sin(i * 0.9 + idx * 1.7)) * 70);
        html += `<i style="height:${h}%"></i>`;
      }
      sp.innerHTML = html;
    });

    // Equalizer: each bar runs its own high-frequency phase set so adjacent
    // bars differ sharply (a jagged spectrum, not a smooth flowing wave).
    type Bar = {
      fill: HTMLElement; cap: HTMLElement; env: number; gain: number;
      o1: number; o2: number; o3: number; p1: number; p2: number; p3: number;
      cur: number; peak: number; drift: number;
    };
    let bars: Bar[] = [];
    let raf = 0;
    let t0 = 0;

    const barCount = () => {
      const w = eqEl?.clientWidth || 360;
      return Math.max(14, Math.min(40, Math.round(w / 15)));
    };
    const build = () => {
      if (!eqEl) return;
      eqEl.innerHTML = '';
      bars = [];
      const N = barCount();
      for (let i = 0; i < N; i++) {
        const bar = document.createElement('div'); bar.className = 'bar';
        const fill = document.createElement('div'); fill.className = 'fill';
        const cap = document.createElement('div'); cap.className = 'cap';
        bar.appendChild(fill); bar.appendChild(cap); eqEl.appendChild(bar);
        const t = i / (N - 1);
        const env = 0.45 + 0.55 * Math.pow(Math.sin(Math.PI * t), 0.6);
        const gain = 0.55 + Math.random() * 0.55;
        const seed = 8 + Math.random() * env * gain * 88;
        fill.style.height = seed.toFixed(1) + '%';
        cap.style.bottom = (seed + 3).toFixed(1) + '%';
        bars.push({
          fill, cap, env, gain,
          o1: 1.4 + Math.random() * 2.6, o2: 3.4 + Math.random() * 3.6, o3: 6 + Math.random() * 5,
          p1: Math.random() * 6.28, p2: Math.random() * 6.28, p3: Math.random() * 6.28,
          cur: Math.random() * 0.5 + 0.2, peak: 0.1, drift: Math.random() * 6.28,
        });
      }
    };
    const frame = (now: number) => {
      const s = (now - t0) / 1000;
      for (const b of bars) {
        b.drift += 0.0011;
        const beat = 0.82 + 0.18 * Math.sin(s * 2.2);
        const raw = 0.5 * Math.sin(s * b.o1 + b.p1) + 0.32 * Math.sin(s * b.o2 + b.p2 + Math.sin(b.drift)) + 0.18 * Math.sin(s * b.o3 + b.p3);
        const target = Math.max(0.03, Math.min(1, (0.5 + 0.5 * raw) * b.env * b.gain * beat));
        const k = target > b.cur ? 0.55 : 0.32;
        b.cur += (target - b.cur) * k;
        b.fill.style.height = (6 + b.cur * 94).toFixed(1) + '%';
        if (b.cur > b.peak) b.peak = b.cur; else b.peak -= 0.016;
        if (b.peak < b.cur) b.peak = b.cur;
        b.cap.style.bottom = (6 + b.peak * 94).toFixed(1) + '%';
      }
      raf = requestAnimationFrame(frame);
    };

    build();
    if (!reduce) {
      t0 = performance.now();
      raf = requestAnimationFrame(frame);
    }

    let rt: ReturnType<typeof setTimeout>;
    const onResize = () => {
      clearTimeout(rt);
      rt = setTimeout(() => {
        if (barCount() !== bars.length) {
          cancelAnimationFrame(raf);
          build();
          if (!reduce) { t0 = performance.now(); raf = requestAnimationFrame(frame); }
        }
      }, 220);
    };
    window.addEventListener('resize', onResize);

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
      if (raf) cancelAnimationFrame(raf);
      window.removeEventListener('resize', onResize);
      if (onMoveOrbs) window.removeEventListener('pointermove', onMoveOrbs);
    };
  });
</script>

<div class="scene" aria-hidden="true">
  <div class="orb a"></div>
  <div class="orb b"></div>
  <div class="orb c"></div>
</div>

<div class="dash" aria-hidden="true">
  <aside class="dash-rail">
    <div class="ri act"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3.5" y="3.5" width="7" height="7" rx="1.6" /><rect x="13.5" y="3.5" width="7" height="7" rx="1.6" /><rect x="3.5" y="13.5" width="7" height="7" rx="1.6" /><rect x="13.5" y="13.5" width="7" height="7" rx="1.6" /></svg></div>
    <div class="ri"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M4 5.5h16v10H9.5l-4 3v-3H4z" /></svg></div>
    <div class="ri"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9.5a6 6 0 0 1 12 0c0 4.5 2 5.5 2 5.5H4s2-1 2-5.5" /><path d="M10 19a2 2 0 0 0 4 0" /></svg></div>
    <div class="ri"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 10v4M9.5 6v12M14.5 8.5v7M19 11v2" /></svg></div>
    <div class="ri"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="8" r="3" /><path d="M3.6 19a5.5 5.5 0 0 1 10.8 0" /><path d="M16 6.6a3 3 0 0 1 0 5.6M20.4 19a5.5 5.5 0 0 0-3.4-5" /></svg></div>
    <div class="ri sett"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M12 2.5v2.5M12 19v2.5M4.4 4.4l1.8 1.8M17.8 17.8l1.8 1.8M2.5 12H5M19 12h2.5M4.4 19.6l1.8-1.8M17.8 6.2l1.8-1.8" /></svg></div>
  </aside>
  <div class="dash-main">
    <div class="dash-top">
      <div class="dlive">Live</div>
      <div class="dview">2,481 watching</div>
    </div>
    <div class="dstats">
      <div class="dpanel dstat"><div class="n"><b>18.2k</b></div><div class="l">Followers</div><div class="spark"></div></div>
      <div class="dpanel dstat"><div class="n">312<b>/m</b></div><div class="l">Chat messages</div><div class="spark"></div></div>
      <div class="dpanel dstat"><div class="n">6:42<b>h</b></div><div class="l">Uptime</div><div class="spark"></div></div>
    </div>

    <div class="dgrid">
      <div class="dpanel chat">
        <div class="ph"><span class="pt">Chat &middot; Auto-mod</span><span class="pdot"></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:42%"></span><span class="gbar" style="width:80%"></span></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:55%"></span><span class="gbar" style="width:64%"></span></span></div>
        <div class="crow flag"><span class="av"></span><span class="ct"><span class="gbar" style="width:38%"></span><span class="gbar" style="width:90%"></span></span></div>
        <div class="cmdwrap" style="margin-top:2px"><span class="cmd">!so</span><span class="cmd">!uptime</span><span class="cmd">!followage</span></div>
      </div>

      <div class="dpanel audio">
        <div class="ph"><span class="pt">Stream audio</span><span class="pdot"></span></div>
        <div class="eq" bind:this={eqEl}></div>
      </div>

      <div class="dpanel followers">
        <div class="ph"><span class="pt">Recent followers</span><span class="pdot"></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:64%"></span></span><span class="gbar" style="width:34px;flex:none"></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:50%"></span></span><span class="gbar" style="width:34px;flex:none"></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:72%"></span></span><span class="gbar" style="width:34px;flex:none"></span></div>
        <div class="crow"><span class="av"></span><span class="ct"><span class="gbar" style="width:44%"></span></span><span class="gbar" style="width:34px;flex:none"></span></div>
      </div>

      <div class="dpanel events">
        <div class="ph"><span class="pt">Activity</span></div>
        <div class="erow"><span class="ei"></span><span class="et"><span class="gbar" style="width:58%"></span><span class="gbar" style="width:34%"></span></span></div>
        <div class="erow"><span class="ei"></span><span class="et"><span class="gbar" style="width:46%"></span><span class="gbar" style="width:28%"></span></span></div>
        <div class="erow"><span class="ei"></span><span class="et"><span class="gbar" style="width:64%"></span><span class="gbar" style="width:40%"></span></span></div>
      </div>
    </div>
  </div>
</div>
<div class="dash-scrim" aria-hidden="true"></div>

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
    <h2 class="reveal d2">Welcome back</h2>
    <p class="lede reveal d2">Sign in to your control room. <br />New here? <a href="/setup">Spin up an instance</a></p>

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

    <p class="legal reveal d8">
      Self-hosted and open source.<br />
      <a href="https://github.com/Luca-Pelzer/engelos" target="_blank" rel="noreferrer noopener">github.com/Luca-Pelzer/engelos</a>
    </p>
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
    --text: #f1f1f3;
    --text-dim: #a6a7ac;
    --text-faint: #6c6d73;
    --field: rgba(255, 255, 255, 0.05);
    --field-focus: rgba(255, 255, 255, 0.08);
    --border: rgba(255, 255, 255, 0.12);
    --border-strong: rgba(255, 255, 255, 0.2);
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

  .dash { position: fixed; inset: 0; z-index: 1; pointer-events: none; overflow: hidden; display: grid; grid-template-columns: 72px 1fr; opacity: 0.62; -webkit-mask-image: radial-gradient(135% 125% at 50% 44%, #000 46%, transparent 88%); mask-image: radial-gradient(135% 125% at 50% 44%, #000 46%, transparent 88%); }
  .dash-rail { border-right: 1px solid rgba(255, 255, 255, 0.06); display: flex; flex-direction: column; align-items: center; gap: 13px; padding: 20px 0; }
  .dash-rail .ri { width: 36px; height: 36px; border-radius: 11px; display: grid; place-items: center; flex: none; background: rgba(255, 255, 255, 0.045); border: 1px solid rgba(255, 255, 255, 0.08); color: rgba(255, 255, 255, 0.42); }
  .dash-rail .ri svg { width: 17px; height: 17px; }
  .dash-rail .ri.act { color: var(--brand); background: color-mix(in srgb, var(--brand) 18%, transparent); border-color: color-mix(in srgb, var(--brand) 40%, transparent); }
  .dash-rail .ri.sett { margin-top: auto; }
  .dash-main { display: flex; flex-direction: column; padding: 24px clamp(20px, 3vw, 40px); gap: 18px; min-width: 0; }
  .dash-top { display: flex; align-items: center; gap: 12px; }
  .dash-top .dlive { display: inline-flex; align-items: center; gap: 7px; font-size: 0.68rem; font-weight: 700; letter-spacing: 0.09em; color: #ff9aa4; background: rgba(255, 77, 94, 0.12); border: 1px solid rgba(255, 77, 94, 0.28); padding: 5px 11px; border-radius: 999px; text-transform: uppercase; }
  .dash-top .dlive::before { content: ''; width: 7px; height: 7px; border-radius: 50%; background: #ff4d5e; }
  .dash-top .dview { margin-left: auto; font-size: 0.8rem; color: rgba(255, 255, 255, 0.4); font-weight: 600; }
  .dpanel { background: rgba(255, 255, 255, 0.035); border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 18px; padding: 18px; display: flex; flex-direction: column; gap: 14px; min-height: 0; }
  .dpanel .ph { display: flex; align-items: center; justify-content: space-between; }
  .dpanel .pt { font-size: 0.7rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; color: rgba(255, 255, 255, 0.42); }
  .dpanel .pdot { width: 8px; height: 8px; border-radius: 50%; background: var(--brand); box-shadow: 0 0 8px var(--brand-glow); }
  .dstats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; }
  .dstat .n { font-size: 1.7rem; font-weight: 800; color: rgba(255, 255, 255, 0.62); letter-spacing: -0.02em; line-height: 1; }
  .dstat .n b { color: var(--brand); }
  .dstat .l { font-size: 0.7rem; color: rgba(255, 255, 255, 0.34); margin-top: 5px; }
  .dstat .spark { display: flex; align-items: flex-end; gap: 3px; height: 26px; margin-top: 10px; }
  .dstat .spark :global(i) { flex: 1; border-radius: 2px; background: linear-gradient(var(--brand), var(--brand-deep)); opacity: 0.55; }
  .dgrid { display: grid; grid-template-columns: 1.2fr 1fr; grid-template-rows: 1fr 1fr; gap: 18px; flex: 1; min-height: 0; }
  .dgrid .dpanel { min-height: 0; overflow: hidden; }
  .gbar { height: 9px; border-radius: 5px; background: rgba(255, 255, 255, 0.1); }
  .crow { display: flex; align-items: center; gap: 11px; }
  .crow .av { width: 28px; height: 28px; border-radius: 50%; flex: none; background: linear-gradient(135deg, rgba(255, 255, 255, 0.22), rgba(255, 255, 255, 0.08)); }
  .crow .ct { flex: 1; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .crow.flag .av { background: linear-gradient(135deg, var(--brand), var(--brand-2)); opacity: 0.8; }
  .erow { display: flex; align-items: center; gap: 11px; }
  .erow .ei { width: 30px; height: 30px; border-radius: 9px; flex: none; background: rgba(255, 255, 255, 0.06); border: 1px solid rgba(255, 255, 255, 0.08); }
  .erow .et { flex: 1; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .cmd { display: inline-flex; align-items: center; gap: 6px; font-size: 0.72rem; font-weight: 600; color: rgba(255, 255, 255, 0.5); background: rgba(255, 255, 255, 0.05); border: 1px solid rgba(255, 255, 255, 0.09); padding: 5px 9px; border-radius: 8px; }
  .cmdwrap { display: flex; flex-wrap: wrap; gap: 8px; }

  .dpanel.audio { justify-content: flex-end; }
  .eq { display: flex; align-items: flex-end; gap: 5px; height: 92px; width: 100%; }
  .eq :global(.bar) { flex: 1 1 0; position: relative; height: 100%; min-width: 0; }
  .eq :global(.bar .fill) { position: absolute; left: 0; right: 0; bottom: 0; height: 8%; border-radius: 3px 3px 0 0; background: linear-gradient(var(--brand-2), var(--brand) 55%, var(--brand-deep)); box-shadow: 0 0 12px -3px var(--brand-glow); will-change: height; }
  .eq :global(.bar .cap) { position: absolute; left: 0; right: 0; height: 2px; border-radius: 2px; bottom: 8%; background: #fff; opacity: 0.85; box-shadow: 0 0 7px var(--brand-glow); will-change: bottom; }

  .dash-scrim { position: fixed; inset: 0; z-index: 2; pointer-events: none; background: radial-gradient(58% 52% at 50% 50%, color-mix(in srgb, var(--scrim) 72%, transparent) 0%, transparent 72%); }

  :global(:root[data-theme='light']) .dash { opacity: 0.72; }
  :global(:root[data-theme='light']) .dash-rail { border-right-color: rgba(20, 40, 70, 0.14); }
  :global(:root[data-theme='light']) .dash-rail .ri:not(.act) { background: rgba(20, 40, 70, 0.05); border-color: rgba(20, 40, 70, 0.14); color: rgba(20, 40, 70, 0.5); }
  :global(:root[data-theme='light']) .dash-rail .ri.act { color: var(--brand); background: color-mix(in srgb, var(--brand) 15%, transparent); border-color: color-mix(in srgb, var(--brand) 42%, transparent); }
  :global(:root[data-theme='light']) .dash-top .dview { color: rgba(20, 40, 70, 0.5); }
  :global(:root[data-theme='light']) .dpanel { background: rgba(255, 255, 255, 0.62); border-color: rgba(20, 40, 70, 0.15); }
  :global(:root[data-theme='light']) .dpanel .pt { color: rgba(20, 40, 70, 0.6); }
  :global(:root[data-theme='light']) .dstat .n { color: rgba(20, 40, 70, 0.78); }
  :global(:root[data-theme='light']) .dstat .l { color: rgba(20, 40, 70, 0.55); }
  :global(:root[data-theme='light']) .gbar { background: rgba(20, 40, 70, 0.2); }
  :global(:root[data-theme='light']) .crow .av { background: linear-gradient(135deg, rgba(20, 40, 70, 0.32), rgba(20, 40, 70, 0.14)); }
  :global(:root[data-theme='light']) .erow .ei { background: rgba(20, 40, 70, 0.08); border-color: rgba(20, 40, 70, 0.15); }
  :global(:root[data-theme='light']) .cmd { color: rgba(20, 40, 70, 0.64); background: rgba(255, 255, 255, 0.6); border-color: rgba(20, 40, 70, 0.16); }
  :global(:root[data-theme='light']) .eq :global(.bar .cap) { background: var(--brand-deep); opacity: 0.7; }

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
  .card .lede a { color: var(--brand); text-decoration: none; font-weight: 600; }
  .card .lede a:hover { text-decoration: underline; }

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
  .legal a { color: var(--text-dim); text-decoration: none; border-bottom: 1px solid var(--border); }
  .legal a:hover { color: var(--text); }

  @keyframes up { from { opacity: 0; transform: translateY(16px); } to { opacity: 1; transform: translateY(0); } }
  .reveal { opacity: 0; animation: up 0.7s var(--lg-ease) forwards; }
  .d1 { animation-delay: 0.04s; } .d2 { animation-delay: 0.1s; } .d3 { animation-delay: 0.16s; } .d4 { animation-delay: 0.22s; }
  .d5 { animation-delay: 0.28s; } .d6 { animation-delay: 0.34s; } .d7 { animation-delay: 0.4s; } .d8 { animation-delay: 0.46s; }

  @media (max-width: 860px) {
    .dash, .dash-scrim { display: none; }
  }
  @media (max-width: 520px) {
    .social { grid-template-columns: 1fr; }
    .card { padding: 1.5rem 1.25rem; }
  }
  @media (prefers-reduced-motion: reduce) {
    .reveal { opacity: 1; animation: none; }
    .orb { animation: none; }
  }
</style>
