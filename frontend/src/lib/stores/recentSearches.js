import { writable } from 'svelte/store';

// Недавние запросы поиска — на пустой странице поиска, как в Spotify. Только
// на этом устройстве (localStorage): у каждого своё, и сервер их не видит.
// Запоминается запрос, по которому что-то открыли или включили, или Enter —
// не каждая буква, пока человек печатает.

const KEY = 'kaikku_recent_searches';
export const MAX_RECENT = 12;

const norm = (q) => q.trim().toLocaleLowerCase();

/** Запрос — первым; тот же (без учёта регистра) — не дублируется. */
export function pushRecent(list, q, max = MAX_RECENT) {
  q = String(q || '').trim();
  if (!q) return list;
  return [q, ...list.filter((x) => norm(x) !== norm(q))].slice(0, max);
}

export function removeRecent(list, q) {
  return list.filter((x) => norm(x) !== norm(q));
}

function load() {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) || '[]');
    return Array.isArray(v) ? v.filter((x) => typeof x === 'string').slice(0, MAX_RECENT) : [];
  } catch {
    return [];
  }
}

export const recentSearches = writable(load());
recentSearches.subscribe((v) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(v));
  } catch {
    /* не запомнится — не страшно */
  }
});

export const rememberSearch = (q) => recentSearches.update((l) => pushRecent(l, q));
