import { readable, writable } from 'svelte/store';

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

export const updateReady = writable(false);
