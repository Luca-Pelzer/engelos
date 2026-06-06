import { API_BASE, getAuthToken } from './api';

export type WsEvent = {
  type: string;
  payload?: unknown;
  ts?: number;
};

export type WsStatus = 'idle' | 'connecting' | 'open' | 'closed' | 'error';

type Listener = (ev: WsEvent) => void;
type StatusListener = (s: WsStatus) => void;

export class EngelWs {
  private url: string;
  private socket: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private statusListeners = new Set<StatusListener>();
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private shouldRun = false;
  private _status: WsStatus = 'idle';

  constructor(path = '/api/v1/ws') {
    const base = API_BASE.replace(/^http/, 'ws');
    this.url = `${base}${path}`;
  }

  get status(): WsStatus {
    return this._status;
  }

  private setStatus(s: WsStatus) {
    this._status = s;
    for (const l of this.statusListeners) l(s);
  }

  connect(): void {
    this.shouldRun = true;
    this.open();
  }

  disconnect(): void {
    this.shouldRun = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.socket) {
      try { this.socket.close(1000, 'client-close'); } catch { /* noop */ }
      this.socket = null;
    }
    this.setStatus('closed');
  }

  on(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  onStatus(listener: StatusListener): () => void {
    this.statusListeners.add(listener);
    listener(this._status);
    return () => this.statusListeners.delete(listener);
  }

  send(ev: WsEvent): void {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(ev));
    }
  }

  private open(): void {
    if (!this.shouldRun) return;
    this.setStatus('connecting');
    const token = getAuthToken();
    const fullUrl = token ? `${this.url}?token=${encodeURIComponent(token)}` : this.url;
    let s: WebSocket;
    try {
      s = new WebSocket(fullUrl);
    } catch {
      this.setStatus('error');
      this.scheduleReconnect();
      return;
    }
    this.socket = s;

    s.onopen = () => {
      this.reconnectAttempts = 0;
      this.setStatus('open');
    };
    s.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data) as WsEvent;
        for (const l of this.listeners) l(data);
      } catch {
        for (const l of this.listeners) l({ type: 'raw', payload: e.data });
      }
    };
    s.onerror = () => {
      this.setStatus('error');
    };
    s.onclose = () => {
      this.socket = null;
      if (this.shouldRun) {
        this.setStatus('closed');
        this.scheduleReconnect();
      }
    };
  }

  private scheduleReconnect(): void {
    if (!this.shouldRun) return;
    if (this.reconnectTimer) return;
    const base = 500;
    const max = 30_000;
    const jitter = Math.random() * 300;
    const delay = Math.min(max, base * 2 ** this.reconnectAttempts) + jitter;
    this.reconnectAttempts += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, delay);
  }
}

export const ws = new EngelWs();
