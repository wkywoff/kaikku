import { auth } from './stores/auth.js';
import { player } from './stores/player.js';

/**
 * «Выйти» (меню аккаунта, настройки): вместе со входом уходят музыка и
 * очередь; App.svelte после этого перезагружает страницу, и с ней — всё,
 * что загружалось для этого входа (главная, кеши страниц, медиатека).
 */
export function signOut() {
  player.clear();
  auth.logout();
}
