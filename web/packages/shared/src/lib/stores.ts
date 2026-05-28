import { writable, derived, type Readable } from 'svelte/store';
import { ws, type WsStatus, type WsEvent } from './ws';

export type User = {
  id: string;
  email: string;
  role: 'owner' | 'admin' | 'mod' | 'viewer';
};

export type Platform = 'twitch' | 'discord' | 'youtube' | 'kick';

export type ConnectionState = {
  platform: Platform;
  connected: boolean;
  username?: string;
};

export const user = writable<User | null>(null);

export const connections = writable<ConnectionState[]>([]);

export const wsStatus = writable<WsStatus>('idle');
ws.onStatus((s) => wsStatus.set(s));

export const events = writable<WsEvent[]>([]);
ws.on((ev) => {
  events.update((list) => {
    const next = [...list, ev];
    return next.length > 200 ? next.slice(next.length - 200) : next;
  });
});

export const botStatus: Readable<{ healthy: boolean; label: string }> = derived(
  [wsStatus, connections],
  ([$ws, $conns]) => {
    if ($ws !== 'open') {
      return { healthy: false, label: $ws === 'connecting' ? 'Connecting…' : 'Offline' };
    }
    const active = $conns.filter((c) => c.connected).map((c) => labelFor(c.platform));
    if (active.length === 0) return { healthy: true, label: 'Online · no platforms connected' };
    if (active.length === 1) return { healthy: true, label: `Connected to ${active[0]}` };
    const last = active[active.length - 1];
    return { healthy: true, label: `Connected to ${active.slice(0, -1).join(', ')} & ${last}` };
  },
);

function labelFor(p: Platform): string {
  switch (p) {
    case 'twitch': return 'Twitch';
    case 'discord': return 'Discord';
    case 'youtube': return 'YouTube';
    case 'kick': return 'Kick';
  }
}

type Toast = { id: number; kind: 'info' | 'success' | 'error' | 'warn'; message: string };
let toastId = 0;
export const toasts = writable<Toast[]>([]);

export function toast(message: string, kind: Toast['kind'] = 'info', durationMs = 4000): void {
  const id = ++toastId;
  toasts.update((t) => [...t, { id, kind, message }]);
  setTimeout(() => {
    toasts.update((t) => t.filter((x) => x.id !== id));
  }, durationMs);
}
