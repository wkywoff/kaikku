import { writable, derived } from 'svelte/store';

const STORAGE_KEY = 'kaikku_settings';

export const DEFAULT_SETTINGS = {
  lang: typeof navigator !== 'undefined' && navigator.language?.startsWith('ru') ? 'ru' : 'en',
  crossfade: false,
  crossfadeSeconds: 4,
  normalize: false,
  trackView: 'list',
  pageTint: 'full',
  animatedBg: true,
  playerTint: 'plate',
  autoplay: false,
};

function load() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
    return sanitize({ ...DEFAULT_SETTINGS, ...saved });
  } catch {
    return { ...DEFAULT_SETTINGS };
  }
}

export function sanitize(s) {
  return {
    lang: s.lang === 'en' ? 'en' : 'ru',
    crossfade: !!s.crossfade,
    crossfadeSeconds: Math.min(12, Math.max(1, Math.round(Number(s.crossfadeSeconds) || 4))),
    normalize: !!s.normalize,
    trackView: s.trackView === 'compact' ? 'compact' : 'list',
    pageTint: s.pageTint === 'header' ? 'header' : 'full',
    animatedBg: s.animatedBg !== false,
    playerTint: ['full', 'plate', 'off'].includes(s.playerTint) ? s.playerTint : 'plate',
    autoplay: !!s.autoplay,
  };
}

function createSettings() {
  const store = writable(load());
  return {
    subscribe: store.subscribe,
    set(key, value) {
      store.update((s) => {
        const next = sanitize({ ...s, [key]: value });
        try {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
        } catch {
        }
        return next;
      });
    },
  };
}

export const settings = createSettings();
export const lang = derived(settings, (s) => s.lang);
