<script lang="ts">
  import { api, ApiException, toast } from '@engelos/shared/lib';
  import { onMount } from 'svelte';

  type Entry = { rank: number; username: string; balance: number };
  type LbResponse = { channel: string; leaderboard: Entry[] };

  const CHANNEL_KEY = 'engelos.channel';

  let channel = $state('');
  let board = $state<Entry[]>([]);
  let loading = $state(false);

  let lookupName = $state('');
  let adjustAmt = $state(100);
  let lookupResult = $state<{ username: string; balance: number } | null>(null);

  function fmt(n: number): string { return n.toLocaleString('de-DE'); }

  async function loadBoard() {
    const ch = channel.trim().toLowerCase();
    if (!ch) { toast('Bitte zuerst einen Kanal-Login eingeben.', 'warn'); return; }
    loading = true;
    try {
      const res = await api.get<LbResponse>(`/api/v1/loyalty/leaderboard?channel=${encodeURIComponent(ch)}`);
      board = res.leaderboard ?? [];
      try { localStorage.setItem(CHANNEL_KEY, ch); } catch { /* ignore */ }
    } catch (err) {
      toast(err instanceof ApiException && err.status === 501 ? 'Loyalty-Feature ist nicht aktiviert.' : 'Laden fehlgeschlagen.', 'error');
    } finally {
      loading = false;
    }
  }

  async function adjust(sign: 1 | -1) {
    const ch = channel.trim().toLowerCase();
    const name = lookupName.trim().toLowerCase();
    if (!ch || !name) { toast('Kanal und Username sind erforderlich.', 'warn'); return; }
    try {
      const res = await api.post<{ username: string; balance: number }>('/api/v1/loyalty/adjust', { channel: ch, username: name, amount: sign * Math.abs(adjustAmt) });
      lookupResult = res;
      board = board.map((e) => (e.username === res.username ? { ...e, balance: res.balance } : e));
      toast(`${sign > 0 ? '+' : '-'}${fmt(Math.abs(adjustAmt))} fuer ${res.username}`, sign > 0 ? 'success' : 'warn');
    } catch (err) {
      toast(err instanceof ApiException && err.status === 409 ? 'Nicht genug Punkte.' : err instanceof ApiException && err.status === 404 ? 'Zuschauer hat noch kein Konto.' : 'Aktion fehlgeschlagen.', 'error');
    }
  }

  onMount(() => {
    try {
      const saved = localStorage.getItem(CHANNEL_KEY);
      if (saved) { channel = saved; void loadBoard(); }
    } catch { /* ignore */ }
  });
</script>

<section class="page" data-screen-label="loyalty">
  <div class="page-wrap">
    <div class="toolbar">
      <div class="input" style="max-width:200px">
        <input type="text" placeholder="Kanal-Login" bind:value={channel} onkeydown={(e) => { if (e.key === 'Enter') loadBoard(); }} />
      </div>
      <button class="btn btn-ghost btn-sm" onclick={loadBoard} disabled={loading}>{loading ? 'Laedt...' : 'Laden'}</button>
    </div>

    <div class="loy-grid">
      <div>
        <div class="section-title">Leaderboard <span class="sub">Top-Zuschauer nach Punkten</span></div>
        <table class="dtable">
          <thead><tr><th style="width:64px">Rang</th><th>Zuschauer</th><th class="right">Punkte</th></tr></thead>
          <tbody>
            {#each board as e (e.username)}
              <tr>
                <td><span class="rank" class:g1={e.rank === 1} class:g2={e.rank === 2} class:g3={e.rank === 3}>{e.rank}</span></td>
                <td><span class="cellname"><span class="n">{e.username}</span></span></td>
                <td class="right num">{fmt(e.balance)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
        {#if board.length === 0}
          <div class="empty">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7 4h10v3a5 5 0 0 1-10 0z" /><path d="M12 12v4M8.5 20h7" /></svg>
            <div class="t">{channel ? 'Noch keine Punkte vergeben' : 'Kanal eingeben und laden'}</div>
          </div>
        {/if}
      </div>

      <div>
        <div class="section-title">Viewer-Lookup</div>
        <div class="card panel">
          <div class="input"><span class="lead"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3-3" /></svg></span><input type="text" placeholder="Username" bind:value={lookupName} /></div>
          {#if lookupResult}
            <div class="lookup-result">
              <div class="nm">{lookupResult.username}</div>
              <div class="pts">{fmt(lookupResult.balance)}</div>
              <div class="pts-l">Punkte</div>
            </div>
          {/if}
          <div class="adjust">
            <span class="input num-in"><input type="number" min="1" bind:value={adjustAmt} /></span>
            <button class="btn btn-primary btn-sm" onclick={() => adjust(1)}>Geben</button>
            <button class="btn btn-ghost btn-sm" onclick={() => adjust(-1)}>Abziehen</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>

<style>
  .loy-grid { display: grid; grid-template-columns: 1.55fr 1fr; gap: 18px; align-items: start; margin-top: 8px; }
  .card { padding: 22px 24px; border-radius: var(--radius); }
  .lookup-result { margin-top: 16px; border-radius: 14px; border: 1px solid var(--panel-border); background: var(--panel-2); padding: 18px; text-align: center; }
  .lookup-result .nm { font-weight: 800; font-size: 1.05rem; }
  .lookup-result .pts { font-size: 2rem; font-weight: 900; letter-spacing: -.03em; margin: 12px 0 2px; font-family: var(--mono); }
  .lookup-result .pts-l { font-size: .78rem; color: var(--text-faint); text-transform: uppercase; letter-spacing: .06em; }
  .adjust { display: flex; gap: 8px; margin-top: 16px; align-items: center; }
  .num-in { width: 96px; }
  .num-in input { text-align: center; font-family: var(--mono); font-weight: 600; }
  @media (max-width: 920px) { .loy-grid { grid-template-columns: 1fr; } }
</style>
