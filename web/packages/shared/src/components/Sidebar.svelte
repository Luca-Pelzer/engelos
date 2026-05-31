<script lang="ts">
  import Logo from './Logo.svelte';

  type Item = { href: string; label: string; icon: string };
  type Props = { current?: string };

  let { current = '/' }: Props = $props();

  const items: Item[] = [
    { href: '/',             label: 'Dashboard',    icon: 'M3 12l9-9 9 9M5 10v10h14V10' },
    { href: '/chat',         label: 'Chat',         icon: 'M4 4h16v12H7l-3 3z' },
    { href: '/commands',     label: 'Commands',     icon: 'M4 6h16M4 12h10M4 18h16' },
    { href: '/redemptions',  label: 'Channel Points', icon: 'M12 2l2.6 6.6L21 9.2l-5 4.3 1.6 6.5L12 16.8 6.4 20l1.6-6.5-5-4.3 6.4-.6z' },
    { href: '/counters',     label: 'Counters',     icon: 'M4 5h16M4 12h16M4 19h16M8 3v18M16 3v18' },
    { href: '/pity',         label: 'Pity',         icon: 'M12 3l2.5 5.5L20 9l-4 4 1 6-5-3-5 3 1-6-4-4 5.5-.5z' },
    { href: '/streak',       label: 'Streak',       icon: 'M13 2L4.5 13H11l-1 9 8.5-11H12z' },
    { href: '/wrapped',      label: 'Wrapped',      icon: 'M12 8v13M12 8L8 4M12 8l4-4M4 8h16v3H4zM6 11v10h12V11' },
    { href: '/automod',      label: 'AutoMod',      icon: 'M12 2l8 4v6c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V6z' },
    { href: '/translate',    label: 'Translate',    icon: 'M4 5h7M9 3v2c0 4-2 7-5 8M5 9c0 3 3 5 6 6M13 19l4-9 4 9M14.5 16h5' },
    { href: '/cohost',       label: 'AI Co-Host',   icon: 'M12 2a5 5 0 015 5v2a5 5 0 01-10 0V7a5 5 0 015-5zM5 21v-1a7 7 0 0114 0v1M9 9h.01M15 9h.01' },
    { href: '/contextmod',   label: 'AI Moderation', icon: 'M12 3l8 4v5c0 5-3.4 8.2-8 10-4.6-1.8-8-5-8-10V7zM9.5 12l1.8 1.8L15 10' },
    { href: '/clipper',      label: 'Auto-Clipper', icon: 'M6 4v16M18 4v16M6 8h12M6 16h12M2 8h4M2 16h4M18 8h4M18 16h4' },
    { href: '/connections',  label: 'Connections',  icon: 'M9 17H7A5 5 0 017 7h2M15 7h2a5 5 0 010 10h-2M8 12h8' },
    { href: '/integrations', label: 'Integrations', icon: 'M10 4h4v6h6v4h-6v6h-4v-6H4v-4h6z' },
    { href: '/settings',     label: 'Settings',     icon: 'M12 8a4 4 0 100 8 4 4 0 000-8zM19.4 15a1.7 1.7 0 00.3 1.9l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-1.9-.3 1.7 1.7 0 00-1 1.5V21a2 2 0 11-4 0v-.1a1.7 1.7 0 00-1.1-1.5 1.7 1.7 0 00-1.9.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.7 1.7 0 00.3-1.9 1.7 1.7 0 00-1.5-1H3a2 2 0 110-4h.1a1.7 1.7 0 001.5-1.1 1.7 1.7 0 00-.3-1.9l-.1-.1a2 2 0 112.8-2.8l.1.1a1.7 1.7 0 001.9.3h.1a1.7 1.7 0 001-1.5V3a2 2 0 114 0v.1a1.7 1.7 0 001 1.5 1.7 1.7 0 001.9-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.7 1.7 0 00-.3 1.9V9a1.7 1.7 0 001.5 1H21a2 2 0 110 4h-.1a1.7 1.7 0 00-1.5 1z' },
  ];

  const isActive = (href: string) =>
    href === '/' ? current === '/' : current === href || current.startsWith(href + '/');
</script>

<aside class="sidebar">
  <div class="px-5 pt-5 pb-4">
    <a href="/" aria-label="engelOS home" class="block">
      <Logo size={26} />
    </a>
  </div>

  <nav class="flex-1 px-2.5 py-2 space-y-0.5">
    {#each items as item (item.href)}
      <a
        href={item.href}
        class="nav-item"
        class:active={isActive(item.href)}
        data-sveltekit-preload-data="hover"
      >
        <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d={item.icon} />
        </svg>
        <span>{item.label}</span>
      </a>
    {/each}
  </nav>

  <div class="px-3 pb-4 pt-2 border-t border-[var(--color-border-soft)] mt-2">
    <a href="/upgrade" class="upgrade-cta" data-sveltekit-preload-data="hover">
      <div class="flex items-center gap-2">
        <span class="upgrade-spark">✦</span>
        <span class="text-[13px] font-semibold tracking-tight">Upgrade to Cloud</span>
      </div>
      <span class="text-[11.5px] text-[var(--color-fg-soft)] leading-snug mt-0.5">
        AI Co-Host · Auto-Clipper · Analytics
      </span>
    </a>
  </div>
</aside>

<style>
  .sidebar {
    width: 232px;
    flex-shrink: 0;
    height: 100vh;
    position: sticky;
    top: 0;
    background: linear-gradient(180deg,
      color-mix(in srgb, var(--color-bg) 95%, var(--color-accent-soft)) 0%,
      var(--color-bg) 200px);
    border-right: 1px solid var(--color-border-soft);
    display: flex;
    flex-direction: column;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    color: var(--color-fg-soft);
    font-size: 13.5px;
    font-weight: 500;
    letter-spacing: -0.005em;
    transition: background var(--duration-fast) var(--ease-out-quad), color var(--duration-fast);
  }
  .nav-item svg {
    width: 17px;
    height: 17px;
    opacity: 0.8;
  }
  .nav-item:hover {
    background: var(--color-surface);
    color: var(--color-fg);
  }
  .nav-item.active {
    background: var(--color-accent-soft);
    color: var(--color-accent-hover);
  }
  .nav-item.active svg {
    opacity: 1;
    color: var(--color-accent);
  }
  .upgrade-cta {
    display: flex;
    flex-direction: column;
    padding: 12px 14px;
    border-radius: var(--radius-lg);
    background:
      radial-gradient(circle at 0% 0%, var(--color-accent-soft) 0%, transparent 70%),
      var(--color-surface);
    border: 1px solid color-mix(in srgb, var(--color-accent) 25%, var(--color-border));
    transition: transform var(--duration-fast), border-color var(--duration-fast);
  }
  .upgrade-cta:hover {
    transform: translateY(-1px);
    border-color: var(--color-accent);
  }
  .upgrade-spark {
    color: var(--color-accent);
    font-size: 14px;
    line-height: 1;
  }
</style>
