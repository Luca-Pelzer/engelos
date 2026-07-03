<script lang="ts">
  type Item = { href: string; label: string; icon: string; dimmed?: boolean };
  type Section = { id: string; label?: string; items: Item[]; dimmed?: boolean };
  type Props = { current?: string };

  let { current = '/' }: Props = $props();

  // Primary navigation collapsed to product pillars. The rail shows only the
  // intended surfaces — Dashboard, the two main features (AI-Mod, Workflow
  // Builder), the Integrations hub, and the operator runtime essentials — plus
  // Settings at the bottom. It is deliberately NOT a per-feature icon list.
  //
  // Everything that used to have its own rail icon is re-homed into a hub and
  // reachable from there, not deleted:
  //   - workflow triggers/actions (/actions /commands /timers /redemptions
  //     /rewards /counters) are indexed by the /workflows hub;
  //   - active bundled plugins (/tts /translate /clipper /quotes) live in the
  //     /integrations "Plugins" section;
  //   - legacy/experimental/template surfaces (/cohost /songrequests /loyalty
  //     /pity /streak /moments /wrapped /liveops) live in a dimmed disclosure
  //     at the bottom of /integrations.
  // All of those routes stay fully functional via direct URL and via their hub
  // link; their pillar icon stays active when they are open (see pillarRoutes).
  // See docs/product/FEATURE_CLASSIFICATION.md and the cleanup plan
  // docs/proposals/engelos-operator-dashboard-product-cleanup.md (§2).

  // Home / overview — sits above the pillar sections.
  const home: Item = {
    href: '/',
    label: 'Dashboard',
    icon: '<rect x="3.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.6"/>',
  };

  const sections: Section[] = [
    {
      // Primary product spine. Keep the rail short and readable: AI-Mod first,
      // Workflow Builder directly below it, then only broad hubs. Individual
      // triggers, plugins, runtime pages, legacy templates, and experiments stay
      // reachable from their hub pages or by direct URL — never as separate rail
      // dots.
      id: 'primary',
      items: [
        { href: '/ai-mod', label: 'AI-Mod', icon: '<path d="M12 3l8 4v5c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V7zM9.5 12l1.8 1.8L15 10"/>' },
        { href: '/workflows', label: 'Workflow Builder', icon: '<rect x="9" y="3.5" width="6" height="5" rx="1.2"/><rect x="3" y="15" width="6" height="5" rx="1.2"/><rect x="15" y="15" width="6" height="5" rx="1.2"/><path d="M12 8.5v3.25M6 15v-3.25h12V15"/>' },
        { href: '/integrations', label: 'Integrations', icon: '<path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z"/><path d="M12 16v5"/>' },
        { href: '/chat', label: 'Runtime', icon: '<path d="M4 5.5h16v10H9.5l-4 3v-3H4z"/><path d="M8 19h8"/>' },
      ],
    },
  ];

  const bottom: Item[] = [
    { href: '/settings', label: 'Settings', icon: '<circle cx="12" cy="12" r="3"/><path d="M12 2.5v2.5M12 19v2.5M4.4 4.4l1.8 1.8M17.8 17.8l1.8 1.8M2.5 12H5M19 12h2.5M4.4 19.6l1.8-1.8M17.8 6.2l1.8-1.8"/>' },
  ];

  // Each visible pillar owns a set of routes in the IA. The single rail icon
  // stays active across all of them so the operator always sees which pillar
  // they are in — even when a re-homed sub-surface (a workflow trigger, a
  // bundled plugin, or a legacy template) is opened by deep link from its hub.
  const pillarRoutes: Record<string, string[]> = {
    '/ai-mod': ['/ai-mod', '/automod', '/contextmod', '/kb'],
    '/workflows': ['/workflows', '/actions', '/commands', '/timers', '/redemptions', '/rewards', '/counters'],
    '/integrations': ['/integrations', '/tts', '/translate', '/clipper', '/quotes', '/cohost', '/songrequests', '/loyalty', '/pity', '/streak', '/moments', '/wrapped', '/liveops'],
    '/chat': ['/chat', '/connections', '/members', '/import'],
  };

  const matches = (route: string) => current === route || current.startsWith(route + '/');

  const isActive = (href: string) => {
    if (href === '/') return current === '/';
    const group = pillarRoutes[href];
    return group ? group.some(matches) : matches(href);
  };
</script>

<aside class="rail">
  <a href="/" class="rail-brand" aria-label="engelOS home">
    <svg viewBox="0 0 64 64" fill="none">
      <defs><linearGradient id="rail-mark" x1="20" y1="14" x2="50" y2="50" gradientUnits="userSpaceOnUse"><stop stop-color="var(--brand)" /><stop offset="1" stop-color="var(--brand-2)" /></linearGradient></defs>
      <path d="M25 18 L48 32 L25 46 Z" fill="url(#rail-mark)" stroke="url(#rail-mark)" stroke-width="9" stroke-linejoin="round" stroke-linecap="round" />
    </svg>
  </a>

  <div class="rail-nav">
    <a href={home.href} class="nav-item" class:active={isActive(home.href)} data-tip={home.label} aria-label={home.label} data-sveltekit-preload-data="hover">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">{@html home.icon}</svg>
    </a>

    {#each sections as section (section.id)}
      {#if section.label}
        <div class="rail-label" class:dimmed={section.dimmed} aria-hidden="true">{section.label}</div>
      {/if}
      {#each section.items as item (item.href)}
        <a href={item.href} class="nav-item" class:active={isActive(item.href)} class:dimmed={item.dimmed ?? section.dimmed} data-tip={item.label} aria-label={item.label} data-sveltekit-preload-data="hover">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">{@html item.icon}</svg>
        </a>
      {/each}
    {/each}
  </div>

  <div class="rail-nav bottom">
    <div class="rail-sep"></div>
    {#each bottom as item (item.href)}
      <a href={item.href} class="nav-item" class:active={isActive(item.href)} data-tip={item.label} aria-label={item.label} data-sveltekit-preload-data="hover">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">{@html item.icon}</svg>
      </a>
    {/each}
  </div>
</aside>
