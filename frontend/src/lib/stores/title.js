import { writable } from 'svelte/store';

// Заголовок вкладки для страниц с данными (альбом, артист, плейлист): страница
// сообщает его, когда загрузится, с ключом своего адреса. App.svelte берёт
// его, только пока открыт тот же адрес, — иначе заголовок по экрану.
export const APP_NAME = 'kaikku';
export const routeTitle = writable(null); // { key: 'album:302127', text }

export function setRouteTitle(key, text) {
  routeTitle.set({ key, text });
}
