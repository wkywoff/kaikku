import { readable, writable } from 'svelte/store';

// Есть ли сеть (плашка «Нет сети», components/StatusBanner.svelte).
export const online = readable(typeof navigator === 'undefined' ? true : navigator.onLine !== false, (set) => {
  if (typeof window === 'undefined') return;
  const up = () => set(true);
  const down = () => set(false);
  window.addEventListener('online', up);
  window.addEventListener('offline', down);
  return () => {
    window.removeEventListener('online', up);
    window.removeEventListener('offline', down);
  };
});

// Вышла новая версия сайта (main.js: сменился сервис-воркер или не
// загрузилась часть старой сборки) — плашка предлагает перезагрузить.
export const updateReady = writable(false);
