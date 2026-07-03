<script lang="ts">
  import { auth, toast } from '@engelos/shared/lib';
  import { theme, setTheme, accent, setAccent, ACCENTS } from '@engelos/shared/lib';
  import { goto } from '$app/navigation';

  type Tab = 'general' | 'account' | 'appearance' | 'security' | 'danger';
  let tab = $state<Tab>('general');

  const tabs: { id: Tab; label: string; danger?: boolean }[] = [
    { id: 'general', label: 'General' },
    { id: 'account', label: 'Account' },
    { id: 'appearance', label: 'Appearance' },
    { id: 'security', label: 'Security' },
    { id: 'danger', label: 'Danger zone', danger: true },
  ];

  async function logout() {
    try {
      await auth.logout();
      goto('/login');
    } catch {
      toast('Could not sign out.', 'error');
    }
  }

  function notYet() {
    toast('This action is not wired up yet.', 'warn');
  }
</script>

<section class="settings">
  <aside class="stabs">
    {#each tabs as t (t.id)}
      <button class="stab" class:on={tab === t.id} class:danger={t.danger} onclick={() => (tab = t.id)}>
        {t.label}
      </button>
    {/each}
  </aside>

  <div class="scontent">
    {#if tab === 'general'}
      <div class="spanel">
        <div class="spanel-head"><h2>General</h2><p>Basic settings of your EngelOS instance.</p></div>
        <div class="card panel">
          <div class="frow">
            <label class="fld" for="instName">Instance name</label>
            <div class="input"><input id="instName" type="text" value="EngelOS" /></div>
            <div class="hint">Shown in the browser tab and in shared links.</div>
          </div>
          <div class="frow split">
            <div class="ftext"><div class="t">Language</div><div class="d">Language of the user interface.</div></div>
            <div class="input" style="min-width:180px"><select><option>English</option><option>Deutsch</option></select></div>
          </div>
          <div class="card-foot">
            <button class="btn btn-primary btn-sm" onclick={notYet}>Save changes</button>
          </div>
        </div>
      </div>
    {/if}

    {#if tab === 'account'}
      <div class="spanel">
        <div class="spanel-head"><h2>Account</h2><p>Manage your sign-in details.</p></div>
        <div class="card panel">
          <div class="frow">
            <label class="fld" for="acctEmail">Email address</label>
            <div class="input"><input id="acctEmail" type="email" placeholder="you@yourdomain.com" /></div>
            <div class="hint">A confirmation mail is sent to the new address.</div>
          </div>
          <div class="card-foot">
            <button class="btn btn-primary btn-sm" onclick={notYet}>Update email</button>
          </div>
        </div>
      </div>
    {/if}

    {#if tab === 'appearance'}
      <div class="spanel">
        <div class="spanel-head"><h2>Appearance</h2><p>Customize the look. Changes apply immediately.</p></div>
        <div class="card panel">
          <div class="frow split">
            <div class="ftext"><div class="t">Theme</div><div class="d">Light or dark.</div></div>
            <div class="theme-seg">
              <button class:on={$theme === 'dark'} onclick={() => setTheme('dark')}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" /></svg>Dark
              </button>
              <button class:on={$theme === 'light'} onclick={() => setTheme('light')}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>Light
              </button>
            </div>
          </div>
          <div class="frow split">
            <div class="ftext"><div class="t">Accent color</div><div class="d">Colors buttons, highlights and live indicators.</div></div>
            <div class="accent-big">
              {#each ACCENTS as a (a.id)}
                <button class:on={$accent.id === a.id} style="background:linear-gradient(135deg,{a.v[0]},{a.v[1]})" title={a.name} aria-label={a.name} onclick={() => setAccent(a)}></button>
              {/each}
            </div>
          </div>
        </div>
      </div>
    {/if}

    {#if tab === 'security'}
      <div class="spanel">
        <div class="spanel-head"><h2>Security</h2><p>Active sessions and account security.</p></div>
        <div class="card panel">
          <div class="spanel-head" style="margin:0 0 10px"><h2 style="font-size:1.05rem">Active session</h2><p>You are signed in on this device.</p></div>
          <div class="sess-row">
            <span class="ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" /></svg></span>
            <span class="meta"><span class="nm">This device <span class="this-tag">Active</span></span><span class="sub">Signed in via OAuth</span></span>
            <button class="btn btn-ghost btn-sm" onclick={logout}>Sign out</button>
          </div>
        </div>
      </div>
    {/if}

    {#if tab === 'danger'}
      <div class="spanel">
        <div class="spanel-head danger-head"><h2>Danger zone</h2><p>Irreversible actions. Use with care.</p></div>
        <div class="card panel danger-card">
          <div class="frow split">
            <div class="ftext"><div class="t">Export data</div><div class="d">Download an archive of your settings and logs as JSON.</div></div>
            <button class="btn btn-ghost btn-sm" onclick={notYet}>Start export</button>
          </div>
          <div class="frow split">
            <div class="ftext"><div class="t">Sign out</div><div class="d">End your current session.</div></div>
            <button class="btn btn-danger btn-sm" onclick={logout}>Sign out</button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</section>

<style>
  .settings { flex: 1; min-height: 0; display: grid; grid-template-columns: 236px 1fr; gap: 0; }
  .stabs { padding: 4px clamp(10px, 1.4vw, 18px) 18px 0; border-right: 1px solid var(--panel-border); overflow-y: auto; display: flex; flex-direction: column; gap: 3px; }
  .stab { display: flex; align-items: center; gap: 11px; padding: 10px 13px; border-radius: 12px; border: 0; background: transparent; cursor: pointer; color: var(--text-dim); font: inherit; font-weight: 600; font-size: .9rem; text-align: left; width: 100%; transition: .16s var(--ease); }
  .stab:hover { background: var(--panel-3); color: var(--text); }
  .stab.on { background: color-mix(in srgb, var(--brand) 15%, transparent); color: var(--brand); }
  .stab.danger.on { background: color-mix(in srgb, var(--bad) 14%, transparent); color: var(--bad); }
  .scontent { overflow-y: auto; padding: 6px clamp(18px, 3vw, 40px) 60px; }
  .spanel { max-width: 680px; margin: 0 0 22px; animation: fade .3s var(--ease); }
  @keyframes fade { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
  .spanel-head { margin: 14px 0 20px; }
  .spanel-head h2 { font-size: 1.3rem; font-weight: 800; letter-spacing: -.025em; }
  .spanel-head p { color: var(--text-dim); font-size: .92rem; margin-top: 5px; line-height: 1.55; }
  .card { padding: 22px 24px; border-radius: var(--radius); }
  .frow { display: flex; flex-direction: column; gap: 0; padding: 16px 0; border-bottom: 1px solid var(--panel-border); }
  .frow:first-child { padding-top: 2px; }
  .frow:last-of-type { border-bottom: 0; padding-bottom: 2px; }
  .frow.split { flex-direction: row; align-items: center; gap: 18px; }
  .frow.split .ftext { flex: 1; min-width: 0; }
  .frow .ftext .t { font-weight: 700; font-size: .95rem; }
  .frow .ftext .d { color: var(--text-dim); font-size: .84rem; margin-top: 3px; line-height: 1.5; }
  .card-foot { display: flex; align-items: center; gap: 14px; margin-top: 20px; padding-top: 18px; border-top: 1px solid var(--panel-border); }
  .sess-row { display: flex; align-items: center; gap: 13px; padding: 13px 0; }
  .sess-row .ic { width: 38px; height: 38px; border-radius: 11px; flex: none; display: grid; place-items: center; border: 1px solid var(--panel-border); background: var(--panel-2); color: var(--text-dim); }
  .sess-row .ic svg { width: 18px; height: 18px; }
  .sess-row .meta { flex: 1; min-width: 0; }
  .sess-row .meta .nm { font-weight: 700; font-size: .9rem; display: flex; align-items: center; gap: 8px; }
  .sess-row .meta .sub { font-size: .8rem; color: var(--text-faint); margin-top: 2px; }
  .this-tag { font-size: .64rem; font-weight: 800; letter-spacing: .04em; text-transform: uppercase; color: var(--ok); background: color-mix(in srgb, var(--ok) 15%, transparent); padding: 2px 7px; border-radius: 5px; }
  .danger-card { border-color: color-mix(in srgb, var(--bad) 30%, transparent); background: color-mix(in srgb, var(--bad) 6%, var(--panel-bg)); }
  .spanel-head.danger-head h2 { color: var(--bad); }
  .accent-big { display: flex; gap: 12px; }
  .accent-big button { width: 42px; height: 42px; border-radius: 12px; border: 2px solid transparent; cursor: pointer; padding: 0; position: relative; transition: .18s; }
  .accent-big button.on { border-color: var(--text); transform: scale(1.05); }
  .theme-seg { display: inline-flex; gap: 4px; background: var(--panel-2); border: 1px solid var(--border); padding: 5px; border-radius: 13px; }
  .theme-seg button { border: 0; background: transparent; cursor: pointer; color: var(--text-dim); font: inherit; font-weight: 600; font-size: .88rem; padding: .6rem 1.1rem; border-radius: 9px; display: inline-flex; align-items: center; gap: .5rem; transition: .16s; }
  .theme-seg button svg { width: 16px; height: 16px; }
  .theme-seg button.on { background: var(--brand); color: var(--on-accent); }
  @media (max-width: 760px) {
    .settings { grid-template-columns: 1fr; }
    .stabs { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--panel-border); padding: 0 12px 12px; }
    .stab { white-space: nowrap; }
  }
</style>
