<script lang="ts">
  type Item = { href: string; label: string; icon: string };
  type Props = { current?: string };

  let { current = '/' }: Props = $props();

  // Icon-rail navigation. Each icon is the inner markup of a 24x24 stroked
  // SVG. Grouped: primary feature pages (top), utility pages (bottom).
  const top: Item[] = [
    { href: '/', label: 'Dashboard', icon: '<rect x="3.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.6"/>' },
    { href: '/chat', label: 'Live-Chat', icon: '<path d="M4 5.5h16v10H9.5l-4 3v-3H4z"/>' },
    { href: '/commands', label: 'Commands', icon: '<path d="M4 6h16M4 12h10M4 18h16"/>' },
    { href: '/loyalty', label: 'Punkte & Games', icon: '<path d="M7 4h10v3a5 5 0 0 1-10 0z"/><path d="M7 5H4v1a3 3 0 0 0 3 3M17 5h3v1a3 3 0 0 1-3 3"/><path d="M12 12v4M8.5 20h7M10 20l.4-2h3.2l.4 2"/>' },
    { href: '/rewards', label: 'Belohnungen', icon: '<rect x="3.5" y="8.5" width="17" height="12" rx="1.6"/><path d="M3.5 12.5h17M12 8.5v12"/>' },
    { href: '/redemptions', label: 'Channel Points', icon: '<path d="M12 3l2.6 6.6L21 9.2l-5 4.3 1.6 6.5L12 16.8 6.4 20l1.6-6.5-5-4.3 6.4-.6z"/>' },
    { href: '/counters', label: 'Counters', icon: '<path d="M4 5h16M4 12h16M4 19h16M8 3v18M16 3v18"/>' },
    { href: '/pity', label: 'Pity', icon: '<path d="M12 3l2.5 5.5L20 9l-4 4 1 6-5-3-5 3 1-6-4-4 5.5-.5z"/>' },
    { href: '/streak', label: 'Streak', icon: '<path d="M13 2L4.5 13H11l-1 9 8.5-11H12z"/>' },
    { href: '/wrapped', label: 'Wrapped', icon: '<path d="M12 8v13M12 8L8 4M12 8l4-4M4 8h16v3H4zM6 11v10h12V11"/>' },
    { href: '/timers', label: 'Auto-Ansagen', icon: '<circle cx="12" cy="13" r="8"/><path d="M12 9v4l2.5 2M9 2.5h6"/>' },
    { href: '/quotes', label: 'Zitate', icon: '<path d="M7 7h4v4a4 4 0 0 1-4 4M13 7h4v4a4 4 0 0 1-4 4"/>' },
    { href: '/liveops', label: 'Event-Plan', icon: '<rect x="3.5" y="5" width="17" height="16" rx="2.5"/><path d="M3.5 9.5h17M8 3v4M16 3v4"/>' },
    { href: '/automod', label: 'AutoMod', icon: '<path d="M12 2l8 4v6c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V6z"/>' },
    { href: '/contextmod', label: 'AI Moderation', icon: '<path d="M12 3l8 4v5c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V7zM9.5 12l1.8 1.8L15 10"/>' },
    { href: '/cohost', label: 'AI Co-Host', icon: '<circle cx="12" cy="8" r="4"/><path d="M5 21v-1a7 7 0 0 1 14 0v1"/>' },
    { href: '/clipper', label: 'Auto-Clipper', icon: '<path d="M6 4v16M18 4v16M6 8h12M6 16h12M2 8h4M18 8h4"/>' },
    { href: '/translate', label: 'Translate', icon: '<path d="M4 5h7M9 3v2c0 4-2 7-5 8M5 9c0 3 3 5 6 6M13 19l4-9 4 9M14.5 16h5"/>' },
    { href: '/songrequests', label: 'Song Requests', icon: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>' },
    { href: '/integrations', label: 'Integrationen', icon: '<path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z"/><path d="M12 16v5"/>' },
    { href: '/connections', label: 'Connections', icon: '<path d="M9 17H7A5 5 0 0 1 7 7h2M15 7h2a5 5 0 0 1 0 10h-2M8 12h8"/>' },
  ];

  const bottom: Item[] = [
    { href: '/upgrade', label: 'Upgrade', icon: '<path d="M12 3l2.4 5 5.6.7-4 4 1 5.5L12 20l-5 2.2 1-5.5-4-4 5.6-.7z"/>' },
    { href: '/settings', label: 'Einstellungen', icon: '<circle cx="12" cy="12" r="3"/><path d="M12 2.5v2.5M12 19v2.5M4.4 4.4l1.8 1.8M17.8 17.8l1.8 1.8M2.5 12H5M19 12h2.5M4.4 19.6l1.8-1.8M17.8 6.2l1.8-1.8"/>' },
  ];

  const isActive = (href: string) =>
    href === '/' ? current === '/' : current === href || current.startsWith(href + '/');
</script>

<aside class="rail">
  <a href="/" class="rail-brand" aria-label="engelOS home">
    <svg viewBox="0 0 64 64" fill="none">
      <defs><linearGradient id="rail-mark" x1="20" y1="14" x2="50" y2="50" gradientUnits="userSpaceOnUse"><stop stop-color="var(--brand)" /><stop offset="1" stop-color="var(--brand-2)" /></linearGradient></defs>
      <path d="M25 18 L48 32 L25 46 Z" fill="url(#rail-mark)" stroke="url(#rail-mark)" stroke-width="9" stroke-linejoin="round" stroke-linecap="round" />
    </svg>
  </a>

  <div class="rail-nav">
    {#each top as item (item.href)}
      <a href={item.href} class="nav-item" class:active={isActive(item.href)} data-tip={item.label} aria-label={item.label} data-sveltekit-preload-data="hover">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">{@html item.icon}</svg>
      </a>
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
