import { auth } from './stores/auth.js';
import { player } from './stores/player.js';
import { recentSearches } from './stores/recentSearches.js';

/** Что осталось на устройстве от этого входа: следующий человек этого не видит. */
export function clearAccountData() {
  player.clear();
  recentSearches.set([]);
}

/**
 * «Выйти» (меню аккаунта, настройки): вместе со входом уходят музыка и
 * очередь; App.svelte после этого перезагружает страницу, и с ней — всё,
 * что загружалось для этого входа (главная, кеши страниц, медиатека).
 */
export function signOut() {
  clearAccountData();
  auth.logout();
}
