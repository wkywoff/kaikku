import { writable } from 'svelte/store';

export const toasts = writable(null);

let seq = 0;
let timer = null;

export function toast(text, kind = 'info', ms = 3000) {
  const id = ++seq;
  toasts.set({ id, text, kind });
  clearTimeout(timer);
  timer = setTimeout(() => toasts.update((t) => (t?.id === id ? null : t)), ms);
}
