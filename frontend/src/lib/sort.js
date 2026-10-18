import { normalizeText } from './rows.js';

// Сортировка списков, как в Spotify: треки в «Любимых» и плейлистах
// (название, исполнитель, альбом, дата добавления, длительность) и
// карточки медиатеки (недавние, добавленные, по алфавиту).

const collators = new Map();
function collator(lang) {
  if (!collators.has(lang)) collators.set(lang, new Intl.Collator(lang, { sensitivity: 'base', numeric: true }));
  return collators.get(lang);
}

const TRACK_KEYS = {
  title: (r) => normalizeText(r.title),
  artist: (r) => normalizeText((r.artists || []).map((a) => a.name).join(', ')),
  album: (r) => normalizeText(r.album?.title),
  added: (r) => (r.addedAt ? Date.parse(r.addedAt) : null),
  duration: (r) => r.durationMs || null,
};

/**
 * Треки по ключу; 'custom' (и неизвестный ключ) — как пришли. desc —
 * по убыванию. Пустые значения — всегда в конце; при равенстве — исходный
 * порядок.
 */
export function sortRows(rows, key, desc = false, lang = 'ru') {
  const get = TRACK_KEYS[key];
  if (!get) return rows;
  return sortBy(rows, get, desc, lang);
}

function sortBy(items, get, desc, lang) {
  const cmp = collator(lang);
  return items
    .map((item, i) => ({ item, i, v: get(item) }))
    .sort((a, b) => {
      const ea = a.v === null || a.v === undefined || a.v === '';
      const eb = b.v === null || b.v === undefined || b.v === '';
      if (ea || eb) return ea === eb ? a.i - b.i : ea ? 1 : -1;
      const c = typeof a.v === 'string' ? cmp.compare(a.v, b.v) : a.v - b.v;
      return (desc ? -c : c) || a.i - b.i;
    })
    .map((x) => x.item);
}

/**
 * Карточки медиатеки: items — [{ key: 'album:ID', name }], times — ответ
 * /api/me/library/times. recent — недавно слушал или добавил, added —
 * недавно добавил, alpha — по алфавиту.
 */
export function sortLibrary(items, mode, times = { added: {}, played: {} }, lang = 'ru') {
  if (mode === 'alpha') return sortBy(items, (x) => normalizeText(x.name), false, lang);
  const at = (k, m) => (m?.[k] ? Date.parse(m[k]) : 0);
  const value =
    mode === 'added' ? (x) => at(x.key, times.added) || null : (x) => Math.max(at(x.key, times.added), at(x.key, times.played)) || null;
  return sortBy(items, value, true, lang);
}
