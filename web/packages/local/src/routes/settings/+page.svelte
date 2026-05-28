<script lang="ts">
  import { Card, Button, Input, Badge } from '@engelos/shared/components';

  let botName = $state('engelOS');
  let listenHost = $state('127.0.0.1');
  let listenPort = $state('8080');
</script>

<section class="space-y-6 max-w-3xl">
  <header class="reveal-up">
    <h2 class="text-xl font-semibold tracking-tight text-fg-strong">Settings</h2>
    <p class="text-[13px] text-fg-soft mt-1">
      Instance, network, security.
    </p>
  </header>

  <Card class="reveal-up reveal-up-delay-1">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Identity</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-5">How your bot introduces itself.</p>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <Input label="Bot display name" bind:value={botName} />
      <Input label="Operator email" placeholder="you@yourdomain.com" type="email" />
    </div>
  </Card>

  <Card class="reveal-up reveal-up-delay-2">
    <div class="flex items-start justify-between mb-1">
      <h3 class="text-[14px] font-semibold tracking-tight text-fg">Network</h3>
      <Badge tone="warn">Local only</Badge>
    </div>
    <p class="text-[12.5px] text-fg-soft mb-5">
      Default binds to <code class="font-mono text-fg">127.0.0.1</code>. Switch to
      <code class="font-mono text-fg">0.0.0.0</code> only behind a reverse proxy.
    </p>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <Input label="Listen host" bind:value={listenHost} />
      <Input label="Listen port" bind:value={listenPort} />
    </div>
  </Card>

  <Card class="reveal-up reveal-up-delay-3">
    <h3 class="text-[14px] font-semibold tracking-tight text-fg">Security</h3>
    <p class="text-[12.5px] text-fg-soft mt-1 mb-5">Lock the bot down before opening it up.</p>
    <ul class="space-y-3 text-[13px]">
      <li class="flex items-center justify-between py-2 border-b border-soft">
        <span class="text-fg">Two-factor authentication</span>
        <Badge tone="neutral">Off</Badge>
      </li>
      <li class="flex items-center justify-between py-2 border-b border-soft">
        <span class="text-fg">API tokens</span>
        <span class="text-fg-soft font-mono text-[12px]">0 active</span>
      </li>
      <li class="flex items-center justify-between py-2">
        <span class="text-fg">Audit log retention</span>
        <span class="text-fg-soft">90 days</span>
      </li>
    </ul>
  </Card>

  <div class="flex justify-end gap-2 pt-2">
    <Button variant="ghost">{#snippet children()}Cancel{/snippet}</Button>
    <Button>{#snippet children()}Save changes{/snippet}</Button>
  </div>
</section>
