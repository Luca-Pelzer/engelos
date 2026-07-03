<script lang="ts">
  import { page } from '$app/stores';

  // The AI-Mod area is one pillar rendered as three coordinated views.
  // This shared sub-nav makes the fast-path -> AI-escalation -> audit story
  // legible no matter which of the three routes the operator lands on.
  // See ENGEL_OS_PRODUCT_CORE.md pipeline (Chat/Event -> fast path -> AI
  // escalation -> action -> audit) and the structure-first target arch §3.
  const tabs = [
    { href: '/ai-mod', label: 'Overview' },
    { href: '/automod', label: 'Fast Path' },
    { href: '/contextmod', label: 'AI Escalation' },
    { href: '/ai-mod/settings', label: 'AI Backend' },
  ] as const;

  const path = $derived($page.url.pathname);
  // '/ai-mod' is exact-match only so the Overview tab does not light up on its
  // sub-routes (e.g. /ai-mod/settings, which has its own tab).
  const isActive = (href: string) =>
    href === '/ai-mod' ? path === href : href === path || path.startsWith(href + '/');
</script>

<nav class="ai-mod-tabs reveal-up" aria-label="AI-Mod sections">
  {#each tabs as tab (tab.href)}
    <a
      href={tab.href}
      class="tab"
      class:active={isActive(tab.href)}
      aria-current={isActive(tab.href) ? 'page' : undefined}
      data-sveltekit-preload-data="hover"
    >
      {tab.label}
    </a>
  {/each}
</nav>

<style>
  .ai-mod-tabs {
    display: flex;
    gap: 4px;
    padding: 4px;
    width: fit-content;
    border-radius: var(--radius-lg);
    background: var(--color-bg-soft);
    border: 1px solid var(--color-border-soft);
  }
  .tab {
    padding: 6px 14px;
    border-radius: var(--radius-md);
    font-size: 13px;
    font-weight: 500;
    color: var(--color-fg-soft);
    text-decoration: none;
    transition: color 150ms var(--ease-out-expo), background 150ms var(--ease-out-expo);
  }
  .tab:hover {
    color: var(--color-fg);
  }
  .tab.active {
    color: var(--color-fg-strong);
    background: var(--color-surface);
    box-shadow: var(--shadow-sm);
  }
</style>
