import { writable } from 'svelte/store';

export const APP_NAME = 'kaikku';
export const routeTitle = writable(null);

export function setRouteTitle(key, text) {
  routeTitle.set({ key, text });
}
