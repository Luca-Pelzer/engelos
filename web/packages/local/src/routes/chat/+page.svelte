<script lang="ts">
  import { Card, Button, EmptyState, Badge } from '@engelos/shared/components';
</script>

<section class="space-y-6">
  <header class="flex items-center justify-between reveal-up">
    <div>
      <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Live Chat</h2>
      <p class="text-[13px] text-fg-soft mt-1">
        Unified feed across all connected platforms.
      </p>
    </div>
    <div class="flex items-center gap-2">
      <Badge tone="neutral" mono>twitch · disconnected</Badge>
      <Badge tone="neutral" mono>discord · disconnected</Badge>
    </div>
  </header>

  <Card class="reveal-up reveal-up-delay-1" padded={false}>
    <div class="grid grid-rows-[1fr_auto] min-h-[480px]">
      <div class="px-5 py-8">
        <EmptyState
          title="No platforms connected yet"
          description="engelOS sits dormant until you connect a chat source. Hook up Twitch or Discord and the feed starts flowing here in real-time."
        >
          {#snippet actions()}
            <Button>
              {#snippet icon()}
                <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M5 3h14l-1.5 11h-4l-2 2v-2H7L5 3z"/></svg>
              {/snippet}
              {#snippet children()}Connect Twitch{/snippet}
            </Button>
            <Button variant="secondary">
              {#snippet icon()}
                <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><circle cx="9" cy="12" r="1.4"/><circle cx="15" cy="12" r="1.4"/><path d="M5 6h14l-1 11-4 2-1-2H7l-1 2-4-2L5 6z" opacity="0.5"/></svg>
              {/snippet}
              {#snippet children()}Connect Discord{/snippet}
            </Button>
          {/snippet}
        </EmptyState>
      </div>

      <div class="chat-input">
        <input type="text" placeholder="Send a message as engelOS…" disabled aria-disabled="true" />
        <span class="text-[11px] text-muted font-mono">⏎</span>
      </div>
    </div>
  </Card>

  <p class="text-[12px] text-muted text-center">
    Tip: mod actions taken in this view sync back to the originating platform.
  </p>
</section>

<style>
  .chat-input {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid var(--color-border-soft);
    background: var(--color-bg-soft);
  }
  .chat-input input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    font-size: 13.5px;
    color: var(--color-fg);
  }
  .chat-input input::placeholder {
    color: var(--color-muted);
  }
  .chat-input input:disabled {
    opacity: 0.55;
  }
</style>
