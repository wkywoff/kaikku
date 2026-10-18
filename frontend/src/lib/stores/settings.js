import { writable, derived } from 'svelte/store';

// Настройки на этом устройстве (localStorage): язык, переход между треками,
// выравнивание громкости, вид треклистов, автовоспроизведение.

const STORAGE_KEY = 'kaikku_settings';

export const DEFAULT_SETTINGS = {
  lang: typeof navigator !== 'undefined' && navigator.language?.startsWith('ru') ? 'ru' : 'en',
  crossfade: true,
  crossfadeSeconds: 4, // 1–12
  // Выключено по умолчанию: треки звучат с той громкостью, что в оригинале.
  normalize: false,
  trackView: 'list', // 'list' | 'compact'
  // Цвет обложки на странице альбома/артиста: 'full' — вся страница (как в
  // Apple Music), 'header' — только шапка, уходящая в серый (как в Spotify).
  pageTint: 'full',
  // Цвета обложки в полноэкранном плеере медленно переливаются.
  animatedBg: true,
  // Когда очередь кончилась — продолжать похожими треками (радио артиста).
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
    autoplay: !!s.autoplay,
  };
}

function createSettings() {
  const store = writable(load());
  return {
    subscribe: store.subscribe,
    /** set('lang', 'en') */
    set(key, value) {
      store.update((s) => {
        const next = sanitize({ ...s, [key]: value });
        try {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
        } catch {
          /* приватный режим и т.п. — настройка просто не переживёт перезагрузку */
        }
        return next;
      });
    },
  };
}

export const settings = createSettings();
export const lang = derived(settings, (s) => s.lang);
