<script lang="ts">
  import { Card, Button, Badge } from '@engelos/shared/components';

  type Integration = {
    id: string;
    name: string;
    description: string;
    category: 'platform' | 'music' | 'gaming' | 'social';
    connected: boolean;
    accent: string;
  };

  const integrations: Integration[] = [
    { id: 'twitch',   name: 'Twitch',    description: 'IRC + Helix + EventSub.',           category: 'platform', connected: false, accent: '#9146ff' },
    { id: 'discord',  name: 'Discord',   description: 'Gateway + Webhooks.',               category: 'platform', connected: false, accent: '#5865f2' },
    { id: 'youtube',  name: 'YouTube',   description: 'Live Chat API.',                    category: 'platform', connected: false, accent: '#ff0033' },
    { id: 'kick',     name: 'Kick',      description: 'Custom WebSocket.',                 category: 'platform', connected: false, accent: '#53fc18' },
    { id: 'spotify',  name: 'Spotify',   description: 'Now-Playing overlay + commands.',   category: 'music',    connected: false, accent: '#1db954' },
    { id: 'steam',    name: 'Steam',     description: 'Currently playing in bio.',         category: 'gaming',   connected: false, accent: '#66c0f4' },
  ];
</script>

<section class="space-y-6">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Integrations</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Wire up platforms, music services, and game APIs.
    </p>
  </header>

  <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
    {#each integrations as it, i (it.id)}
      <Card interactive class="reveal-up reveal-up-delay-{Math.min(i + 1, 5)}">
        <div class="flex items-start justify-between gap-3">
          <span class="integ-icon" style="--c: {it.accent}">
            {it.name[0]}
          </span>
          {#if it.connected}
            <Badge tone="success">Connected</Badge>
          {:else}
            <Badge tone="neutral">Not set up</Badge>
          {/if}
        </div>
        <div class="mt-4">
          <h3 class="text-[14.5px] font-semibold tracking-tight text-fg-strong">{it.name}</h3>
          <p class="text-[12.5px] text-fg-soft mt-1 leading-relaxed">{it.description}</p>
        </div>
        <div class="mt-4 pt-3 border-t border-soft flex items-center justify-between">
          <span class="text-[11px] uppercase tracking-wider text-muted font-medium">
            {it.category}
          </span>
          <Button variant="ghost" size="sm">
            {#snippet children()}{it.connected ? 'Manage' : 'Connect'}{/snippet}
          </Button>
        </div>
      </Card>
    {/each}
  </div>
</section>

<style>
  .integ-icon {
    width: 36px;
    height: 36px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--c) 18%, transparent);
    color: var(--c);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 17px;
    border: 1px solid color-mix(in srgb, var(--c) 30%, transparent);
  }
</style>
