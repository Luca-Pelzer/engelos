import { writable } from 'svelte/store';

export type Theme = 'dark' | 'light';

const STORAGE_KEY = 'engelos_theme';

// The meta theme-color must track the active surface so the mobile browser
// chrome matches the app background instead of flashing a stale color.
const META_COLOR: Record<Theme, string> = {
  dark: '#08090c',
  light: '#f6f8fb',
};

function readStored(): Theme {
  if (typeof document === 'undefined') return 'dark';
  const attr = document.documentElement.getAttribute('data-theme');
  if (attr === 'light' || attr === 'dark') return attr;
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    /* localStorage unavailable (private mode); fall through to default */
  }
  return 'dark';
}

function apply(theme: Theme): void {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('data-theme', theme);
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', META_COLOR[theme]);
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    /* ignore persistence failure */
  }
}

export const theme = writable<Theme>(readStored());

export function setTheme(next: Theme): void {
  apply(next);
  theme.set(next);
}

export function toggleTheme(): void {
  theme.update((current) => {
    const next: Theme = current === 'dark' ? 'light' : 'dark';
    apply(next);
    return next;
  });
}

// --- Accent palette (shared across login + dashboard) ---
// Each accent is a [primary, secondary] gradient pair written to the
// --brand / --brand-2 custom properties. Magma is the default brand color.
export type Accent = { id: string; name: string; v: [string, string] };

export const ACCENTS: Accent[] = [
  { id: 'magma', name: 'Magma', v: ['#ff5d73', '#ff9e3d'] },
  { id: 'aurora', name: 'Aurora', v: ['#1fe3b3', '#34c7ff'] },
  { id: 'violet', name: 'Violet', v: ['#8b5cff', '#43a6ff'] },
  { id: 'lime', name: 'Lime', v: ['#b6f23d', '#19d3a2'] },
];

const ACCENT_KEY = 'engelos-accent';

function readAccent(): Accent {
  if (typeof document === 'undefined') return ACCENTS[0];
  try {
    const saved = localStorage.getItem(ACCENT_KEY);
    if (saved) {
      const v = JSON.parse(saved) as [string, string];
      const match = ACCENTS.find((a) => a.v[0] === v[0]);
      if (match) return match;
    }
  } catch {
    /* localStorage unavailable; fall through to default */
  }
  return ACCENTS[0];
}

function applyAccent(a: Accent): void {
  if (typeof document === 'undefined') return;
  document.documentElement.style.setProperty('--brand', a.v[0]);
  document.documentElement.style.setProperty('--brand-2', a.v[1]);
  try {
    localStorage.setItem(ACCENT_KEY, JSON.stringify(a.v));
  } catch {
    /* ignore persistence failure */
  }
}

export const accent = writable<Accent>(readAccent());

export function setAccent(next: Accent): void {
  applyAccent(next);
  accent.set(next);
}
