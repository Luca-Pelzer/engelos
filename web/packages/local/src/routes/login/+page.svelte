<script lang="ts">
  import { Card, Button, Input, Logo } from '@engelos/shared/components';
  import { auth, ApiException, setAuthToken, API_BASE } from '@engelos/shared/lib';
  import { toast } from '@engelos/shared/lib';
  import { goto } from '$app/navigation';

  const twitchLoginUrl = `${API_BASE}/api/v1/auth/twitch/login?purpose=user`;

  let email = $state('');
  let password = $state('');
  let loading = $state(false);
  let errors = $state<{ email?: string; password?: string }>({});

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    errors = {};
    if (!email) errors.email = 'Required';
    if (!password) errors.password = 'Required';
    if (Object.keys(errors).length > 0) return;

    loading = true;
    try {
      const res = await auth.login({ email, password });
      setAuthToken(res.token);
      toast('Welcome back.', 'success');
      goto('/');
    } catch (err) {
      const msg = err instanceof ApiException
        ? (err.status === 0 ? 'Cannot reach the engelOS daemon. Is it running on :8080?' : err.message)
        : 'Login failed. Check your credentials.';
      toast(msg, 'error', 6000);
    } finally {
      loading = false;
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center px-6 py-10 grid-noise relative overflow-hidden">
  <div class="aura aura-a" aria-hidden="true"></div>
  <div class="aura aura-b" aria-hidden="true"></div>

  <div class="w-full max-w-[400px] relative z-10">
    <div class="flex justify-center mb-7 reveal-up">
      <Logo size={36} />
    </div>

    <Card class="reveal-up reveal-up-delay-1 backdrop-blur-sm" padded={false}>
      <form onsubmit={handleSubmit} class="p-7">
        <h1 class="text-[20px] font-semibold tracking-tight text-fg-strong mb-1">Sign in</h1>
        <p class="text-[13px] text-fg-soft mb-6">
          Run your bot. Own your data.
        </p>

        <div class="space-y-4">
          <Input
            label="Email"
            type="email"
            autocomplete="email"
            placeholder="you@yourdomain.com"
            bind:value={email}
            error={errors.email}
            required
          />
          <Input
            label="Password"
            type="password"
            autocomplete="current-password"
            placeholder="••••••••"
            bind:value={password}
            error={errors.password}
            required
          />
        </div>

        <Button class="mt-6" type="submit" fullWidth size="lg" loading={loading}>
          {#snippet children()}Sign in{/snippet}
        </Button>

        <div class="my-5 flex items-center gap-3">
          <span class="h-px flex-1 bg-[var(--color-border-soft)]"></span>
          <span class="text-[11.5px] text-fg-soft">or</span>
          <span class="h-px flex-1 bg-[var(--color-border-soft)]"></span>
        </div>

        <a href={twitchLoginUrl} class="twitch-btn" data-sveltekit-reload>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true">
            <path d="M4 2L2.5 5.5v13H7V22h3l3-3h4l5-5V2zm15 11l-3 3h-4l-3 3v-3H7V4h12zM15 7h-2v5h2zm-5 0H8v5h2z"/>
          </svg>
          <span>Login with Twitch</span>
        </a>

        <div class="mt-6 pt-5 border-t border-soft text-center">
          <p class="text-[12.5px] text-fg-soft">
            First time?
            <a href="/setup" class="text-accent hover:underline font-medium">
              Run the setup wizard →
            </a>
          </p>
        </div>
      </form>
    </Card>

    <footer class="mt-7 flex items-center justify-between text-[11.5px] text-muted reveal-up reveal-up-delay-2">
      <span class="font-mono">engelOS v0.1.0</span>
      <a
        href="https://github.com/engelos/engelos"
        target="_blank"
        rel="noreferrer noopener"
        class="hover:text-fg-soft transition-colors"
      >
        github.com/engelos
      </a>
    </footer>
  </div>
</div>

<style>
  .twitch-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 9px;
    width: 100%;
    padding: 11px 14px;
    border-radius: var(--radius-md);
    background: #9146ff;
    color: #fff;
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.01em;
    transition: background var(--duration-fast) var(--ease-out-quad), transform var(--duration-fast);
  }
  .twitch-btn:hover {
    background: #7c2fff;
    transform: translateY(-1px);
  }
  .aura {
    position: absolute;
    border-radius: 50%;
    filter: blur(80px);
    pointer-events: none;
  }
  .aura-a {
    width: 420px;
    height: 420px;
    background: color-mix(in srgb, var(--color-accent) 25%, transparent);
    top: -120px;
    left: -120px;
  }
  .aura-b {
    width: 320px;
    height: 320px;
    background: color-mix(in srgb, var(--color-info) 16%, transparent);
    bottom: -100px;
    right: -80px;
  }
</style>
