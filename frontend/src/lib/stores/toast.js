import { writable } from 'svelte/store';

// Короткое уведомление внизу экрана («Добавлено в Любимые треки»), как в
// Spotify. Новое заменяет старое; само пропадает через 3 с (или через ms).
export const toasts = writable(null); // { id, text, kind: 'info' | 'error' }

let seq = 0;
let timer = null;

export function toast(text, kind = 'info', ms = 3000) {
  const id = ++seq;
  toasts.set({ id, text, kind });
  clearTimeout(timer);
  timer = setTimeout(() => toasts.update((t) => (t?.id === id ? null : t)), ms);
}
