<script lang="ts">
  let bill = $state<'monthly' | 'yearly'>('monthly');
  const prices = { cloud: { monthly: 12, yearly: 10 }, pro: { monthly: 29, yearly: 24 } };

  const cloudPrice = $derived(prices.cloud[bill]);
  const proPrice = $derived(prices.pro[bill]);
  const billedLabel = (k: 'cloud' | 'pro') =>
    bill === 'yearly' ? `jaehrlich, ${prices[k].yearly * 12} EUR/Jahr` : 'monatlich abgerechnet';

  const rows: [string, string, string, string][] = [
    ['Verbundene Plattformen', '2', 'Unbegrenzt', 'Unbegrenzt'],
    ['Moderatoren-Sitze', '1', '5', 'Unbegrenzt'],
    ['Chat-Verlauf', '7 Tage', '90 Tage', 'Unbegrenzt'],
    ['Auto-Mod und Filter', 'yes', 'yes', 'yes'],
    ['Custom Overlays', 'no', 'yes', 'yes'],
    ['Auto-Backups', 'no', 'yes', 'yes'],
    ['Team-Rollen und Audit-Log', 'no', 'no', 'yes'],
    ['Priority-Support (SLA)', 'no', 'no', 'yes'],
  ];
</script>

<section class="up-scroll" data-screen-label="upgrade">
  <div class="up-hero">
    <span class="up-eyebrow">Plaene und Cloud</span>
    <h2>Dein Control Room, <span class="grad">ohne Limits</span></h2>
    <p>Starte kostenlos auf deiner eigenen Hardware oder lass EngelOS in der Cloud laufen, mit Auto-Updates, Backups und Team-Funktionen.</p>
    <div class="bill">
      <button class:on={bill === 'monthly'} onclick={() => (bill = 'monthly')}>Monatlich</button>
      <button class:on={bill === 'yearly'} onclick={() => (bill = 'yearly')}>Jaehrlich <span class="tag">-20%</span></button>
    </div>
  </div>

  <div class="plans">
    <div class="plan">
      <div class="pname">Self-Hosted</div>
      <div class="ptag">Open-Source. Laeuft auf deinem eigenen Server.</div>
      <div class="price"><span class="cur">EUR</span><span class="amt">0</span><span class="per">/ fuer immer</span></div>
      <div class="billed">Keine Kreditkarte noetig</div>
      <div class="cta"><button class="btn btn-ghost">Selbst hosten</button></div>
      <ul>
        <li><b>2</b> Plattformen verbinden</li>
        <li>Live-Chat und Moderation</li>
        <li>7 Tage Chat-Verlauf</li>
        <li>Community-Support</li>
      </ul>
    </div>

    <div class="plan feat">
      <span class="popular">Beliebt</span>
      <div class="pname">Cloud</div>
      <div class="ptag">Gehostet, gewartet und gesichert von uns.</div>
      <div class="price"><span class="cur">EUR</span><span class="amt">{cloudPrice}</span><span class="per">/ Monat</span></div>
      <div class="billed">{billedLabel('cloud')}</div>
      <div class="cta"><button class="btn btn-primary">Cloud starten</button></div>
      <ul>
        <li><b>Unbegrenzt</b> Plattformen</li>
        <li><b>5</b> Moderatoren-Sitze</li>
        <li>90 Tage Chat-Verlauf</li>
        <li>Custom Overlays und API</li>
        <li>Auto-Backups</li>
      </ul>
    </div>

    <div class="plan">
      <div class="pname">Pro</div>
      <div class="ptag">Fuer Teams, Netzwerke und Agenturen.</div>
      <div class="price"><span class="cur">EUR</span><span class="amt">{proPrice}</span><span class="per">/ Monat</span></div>
      <div class="billed">{billedLabel('pro')}</div>
      <div class="cta"><button class="btn btn-ghost">Pro waehlen</button></div>
      <ul>
        <li><b>Alles aus Cloud</b>, plus:</li>
        <li><b>Unbegrenzt</b> Mod-Sitze</li>
        <li>Team-Rollen und Audit-Log</li>
        <li>Unbegrenzter Verlauf</li>
        <li>Priority-Support (SLA)</li>
      </ul>
    </div>
  </div>

  <div class="cmp-wrap">
    <div class="cmp-title">Plaene im Vergleich</div>
    <div class="cmp-sub">Alle Plaene enthalten Auto-Mod, Filter und unbegrenzte Zuschauer.</div>
    <table class="cmp">
      <thead><tr><th class="row-h">Funktion</th><th>Self-Hosted</th><th class="feat">Cloud</th><th>Pro</th></tr></thead>
      <tbody>
        {#each rows as r (r[0])}
          <tr>
            <td class="row-h">{r[0]}</td>
            {#each [r[1], r[2], r[3]] as cell}
              <td>
                {#if cell === 'yes'}<span class="yes">+</span>{:else if cell === 'no'}<span class="no">-</span>{:else}{cell}{/if}
              </td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  <p class="up-foot">Preise zzgl. MwSt. Jederzeit kuendbar. Beim Wechsel von Self-Hosted zu Cloud werden deine Einstellungen automatisch migriert.</p>
</section>

<style>
  .up-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 0 clamp(18px, 3vw, 40px) 64px; }
  .up-hero { text-align: center; max-width: 680px; margin: clamp(18px, 4vh, 46px) auto clamp(20px, 3vh, 34px); }
  .up-eyebrow { display: inline-flex; align-items: center; gap: 7px; font-size: .74rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; color: var(--brand); background: color-mix(in srgb, var(--brand) 14%, transparent); border: 1px solid color-mix(in srgb, var(--brand) 32%, transparent); padding: 6px 13px; border-radius: 99px; margin-bottom: 18px; }
  .up-hero h2 { font-size: clamp(1.9rem, 3.6vw, 2.7rem); font-weight: 900; letter-spacing: -.035em; line-height: 1.05; }
  .up-hero h2 .grad { background: linear-gradient(105deg, var(--brand), var(--brand-2)); -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent; }
  .up-hero p { color: var(--text-dim); font-size: 1.02rem; margin-top: 14px; line-height: 1.6; }
  .bill { display: inline-flex; align-items: center; gap: 5px; background: var(--panel-2); border: 1px solid var(--border); padding: 5px; border-radius: 13px; margin-top: 22px; }
  .bill button { border: 0; background: transparent; cursor: pointer; color: var(--text-dim); font: inherit; font-weight: 600; font-size: .88rem; padding: .55rem 1rem; border-radius: 9px; transition: .16s; display: inline-flex; align-items: center; gap: 7px; }
  .bill button.on { background: var(--brand); color: var(--on-accent); }
  .bill .tag { font-size: .66rem; font-weight: 800; letter-spacing: .03em; padding: 2px 6px; border-radius: 5px; background: color-mix(in srgb, var(--brand) 22%, transparent); color: var(--brand); }
  .bill button.on .tag { background: rgba(0,0,0,.18); color: var(--on-accent); }
  .plans { display: grid; grid-template-columns: repeat(3, 1fr); gap: 18px; max-width: 1040px; margin: 8px auto 0; align-items: stretch; }
  .plan { position: relative; display: flex; flex-direction: column; padding: 26px 24px; border-radius: var(--radius); background: var(--panel-bg); border: 1px solid var(--panel-border); box-shadow: var(--panel-shadow); overflow: hidden; }
  .plan::before { content: ""; position: absolute; inset: 0; border-radius: inherit; pointer-events: none; background: linear-gradient(180deg, var(--hi), transparent 20%); opacity: .5; }
  .plan > * { position: relative; }
  .plan.feat { border-color: color-mix(in srgb, var(--brand) 55%, transparent); box-shadow: 0 0 0 1px color-mix(in srgb, var(--brand) 40%, transparent), 0 40px 90px -40px var(--brand-glow); transform: translateY(-6px); }
  .popular { position: absolute; top: 16px; right: 16px; font-size: .66rem; font-weight: 800; letter-spacing: .05em; text-transform: uppercase; color: var(--on-accent); background: linear-gradient(105deg, var(--brand), var(--brand-2)); padding: 4px 10px; border-radius: 99px; box-shadow: 0 6px 16px -6px var(--brand-glow); }
  .plan .pname { font-size: 1.15rem; font-weight: 800; letter-spacing: -.02em; }
  .plan .ptag { color: var(--text-dim); font-size: .86rem; margin-top: 4px; min-height: 2.6em; line-height: 1.4; }
  .plan .price { display: flex; align-items: flex-end; gap: 4px; margin: 16px 0 2px; }
  .plan .price .cur { font-size: 1.05rem; font-weight: 700; color: var(--text-dim); margin-bottom: .35em; }
  .plan .price .amt { font-size: 2.7rem; font-weight: 900; letter-spacing: -.04em; line-height: .9; }
  .plan .price .per { color: var(--text-faint); font-size: .86rem; margin-bottom: .45em; }
  .plan .billed { font-size: .78rem; color: var(--text-faint); min-height: 1.2em; }
  .plan .cta { margin: 20px 0 18px; }
  .plan .cta .btn { width: 100%; }
  .plan ul { list-style: none; display: flex; flex-direction: column; gap: 11px; }
  .plan li { display: flex; align-items: flex-start; gap: 10px; font-size: .88rem; color: var(--text-dim); line-height: 1.4; }
  .plan li b { color: var(--text); font-weight: 700; }
  .cmp-wrap { max-width: 1040px; margin: 48px auto 0; }
  .cmp-title { font-size: 1.3rem; font-weight: 800; letter-spacing: -.02em; text-align: center; margin-bottom: 6px; }
  .cmp-sub { text-align: center; color: var(--text-dim); font-size: .9rem; margin-bottom: 24px; }
  .cmp { width: 100%; border-collapse: collapse; background: var(--panel-bg); border: 1px solid var(--panel-border); border-radius: var(--radius); overflow: hidden; }
  .cmp th, .cmp td { padding: 14px 18px; text-align: center; border-bottom: 1px solid var(--panel-border); font-size: .9rem; }
  .cmp thead th { font-weight: 800; font-size: .95rem; background: var(--panel-2); }
  .cmp thead th.feat { color: var(--brand); }
  .cmp th.row-h, .cmp td.row-h { text-align: left; font-weight: 600; color: var(--text); width: 38%; }
  .cmp td { color: var(--text-dim); }
  .cmp tbody tr:last-child td { border-bottom: 0; }
  .cmp .yes { color: var(--brand); font-weight: 800; }
  .cmp .no { color: var(--text-faint); }
  .up-foot { text-align: center; color: var(--text-faint); font-size: .84rem; margin: 34px auto 0; max-width: 560px; line-height: 1.6; }
  @media (max-width: 860px) {
    .plans { grid-template-columns: 1fr; max-width: 420px; }
    .plan.feat { transform: none; }
    .cmp-wrap { overflow-x: auto; }
    .cmp { min-width: 600px; }
  }
</style>
