import { readable, writable } from 'svelte/store';
import { paths } from '../route.js';

const STORAGE_KEY = 'kaikku_side_player';

export const SIDE_MIN = 280;
export const SIDE_MAX = 420;
export const SIDE_DEFAULT = 400;
export const SIDE_MAX_SHARE = 0.4;
export const SIDE_FITS_QUERY = '(min-width: 1000px)';

export function maxWidthFor(viewport) {
  return Math.max(SIDE_MIN, Math.min(SIDE_MAX, Math.floor(viewport * SIDE_MAX_SHARE)));
}

export function clampWidth(width, viewport = Infinity) {
  const n = Number(width);
  const w = Number.isFinite(n) ? n : SIDE_DEFAULT;
  return Math.round(Math.min(maxWidthFor(viewport), Math.max(SIDE_MIN, w)));
}

export function parseSaved(raw) {
  try {
    const saved = JSON.parse(raw || '{}');
    return { open: saved.open === true, width: clampWidth(saved.width) };
  } catch {
    return { open: false, width: SIDE_DEFAULT };
  }
}

const CONTEXT_PATHS = {
  album: (id) => paths.album(id),
  playlist: (id) => paths.playlist(id),
  mix: (id) => paths.mix(id),
  artist: (id) => paths.artist(id),
  liked: () => paths.liked(),
};

export function playingFrom(entry, tr) {
  const ctx = entry?.context;
  if (!ctx || !CONTEXT_PATHS[ctx.kind]) return null;
  const title = ctx.kind === 'liked' ? tr('liked.title') : ctx.title || (ctx.kind === 'album' ? entry.album : '');
  if (!title) return null;
  return { title, href: ctx.kind === 'liked' || ctx.id ? CONTEXT_PATHS[ctx.kind](ctx.id) : null };
}

function readSaved() {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function createSidePlayer() {
  const store = writable(parseSaved(readSaved()));
  let current;
  store.subscribe((s) => (current = s));
  const save = () => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(current));
      return true;
    } catch {
      return false;
    }
  };
  const setOpen = (open) => {
    store.update((s) => ({ ...s, open }));
    save();
  };
  return {
    subscribe: store.subscribe,
    open: () => setOpen(true),
    close: () => setOpen(false),
    resize(width) {
      store.update((s) => ({ ...s, width: clampWidth(width) }));
    },
    commitWidth: save,
  };
}

export const sidePlayer = createSidePlayer();

export const sideFits = readable(false, (set) => {
  if (typeof matchMedia !== 'function') return;
  const query = matchMedia(SIDE_FITS_QUERY);
  const update = () => set(query.matches);
  update();
  query.addEventListener('change', update);
  return () => query.removeEventListener('change', update);
});
