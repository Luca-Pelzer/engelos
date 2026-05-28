<script lang="ts">
  import { Card, Button, Badge, EmptyState } from '@engelos/shared/components';

  type Command = { name: string; reply: string; cooldown: string; uses: number };
  const commands: Command[] = [
    { name: '!so',        reply: 'Big shout-out to {target}! Go follow → twitch.tv/{target}', cooldown: '30s',  uses: 142 },
    { name: '!discord',   reply: 'Hang out with us: {discord_invite}',                         cooldown: '5min', uses:  87 },
    { name: '!lurk',      reply: '{user} is lurking. Respect the silent ones.',                cooldown: '10s',  uses:  41 },
    { name: '!socials',   reply: 'twitter.com/{streamer} · youtube.com/{streamer}',            cooldown: '5min', uses:  29 },
  ];
</script>

<section class="space-y-6">
  <header class="flex items-end justify-between reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Commands</h2>
      <p class="text-[13px] text-fg-soft mt-1">
        Text replies, cooldowns, role gates. The classics.
      </p>
    </div>
    <Button>
      {#snippet icon()}
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
      {/snippet}
      {#snippet children()}New command{/snippet}
    </Button>
  </header>

  <Card padded={false} class="reveal-up reveal-up-delay-1">
    {#if commands.length === 0}
      <EmptyState
        title="No commands yet"
        description="Even a simple !discord makes the bot feel alive."
      />
    {:else}
      <table class="w-full text-left">
        <thead>
          <tr class="text-[11px] uppercase tracking-wider text-muted">
            <th class="px-5 py-3 font-medium">Trigger</th>
            <th class="px-5 py-3 font-medium">Reply</th>
            <th class="px-5 py-3 font-medium">Cooldown</th>
            <th class="px-5 py-3 font-medium text-right">Uses</th>
            <th class="px-5 py-3 font-medium"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--color-border-soft)]">
          {#each commands as c (c.name)}
            <tr class="hover:bg-[var(--color-bg-soft)] transition-colors">
              <td class="px-5 py-3.5">
                <span class="font-mono text-[13px] text-accent">{c.name}</span>
              </td>
              <td class="px-5 py-3.5 text-[13px] text-fg-soft max-w-[420px] truncate">
                {c.reply}
              </td>
              <td class="px-5 py-3.5">
                <Badge tone="neutral" mono>{c.cooldown}</Badge>
              </td>
              <td class="px-5 py-3.5 text-right font-mono text-[13px] tabular-nums text-fg">
                {c.uses}
              </td>
              <td class="px-5 py-3.5 text-right">
                <button class="text-[12px] text-fg-soft hover:text-accent transition-colors">
                  Edit
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Card>
</section>
