import { writable } from 'svelte/store';

// Как отсортированы списки — запоминается на этом устройстве, отдельно для
// медиатеки, «Любимых» и плейлистов (один выбор на все плейлисты, как в Spotify).
const KEY = 'kaikku_sort';
const DEFAULTS = {
  library: 'recent', // recent | added | alpha
  liked: { key: 'added', desc: true },
  playlist: { key: 'custom', desc: false },
};

function load() {
  try {
    const p = JSON.parse(localStorage.getItem(KEY) || '{}');
    const pick = (v, d) => (v && typeof v.key === 'string' ? { key: v.key, desc: !!v.desc } : d);
    return {
      library: ['recent', 'added', 'alpha'].includes(p.library) ? p.library : DEFAULTS.library,
      liked: pick(p.liked, DEFAULTS.liked),
      playlist: pick(p.playlist, DEFAULTS.playlist),
    };
  } catch {
    return { ...DEFAULTS };
  }
}

export const sortPrefs = writable(load());
sortPrefs.subscribe((v) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(v));
  } catch {
    /* не запомнится — не страшно */
  }
});
