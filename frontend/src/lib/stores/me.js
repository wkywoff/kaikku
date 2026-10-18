import { writable, derived } from 'svelte/store';
import { clearPageCaches } from '../lru.js';

// Текущий пользователь ({ id, username, is_admin }) — App.svelte загружает
// его после входа.
export const me = writable(null);
export const isAdmin = derived(me, ($me) => !!$me?.is_admin);

// Режим админа включается в меню аккаунта (на этом устройстве). Пока он
// выключен, админ видит сайт как все — без карандашей правки.
const ADMIN_MODE_KEY = 'kaikku_admin_mode';

function loadAdminMode() {
  try {
    return localStorage.getItem(ADMIN_MODE_KEY) === '1';
  } catch {
    return false;
  }
}

export const adminMode = writable(loadAdminMode());
adminMode.subscribe((on) => {
  try {
    localStorage.setItem(ADMIN_MODE_KEY, on ? '1' : '0');
  } catch {
    /* приватный режим — просто не запомнится */
  }
});

/** Показывать ли правку: админ и режим админа включён. */
export const canEdit = derived([isAdmin, adminMode], ([$isAdmin, $adminMode]) => $isAdmin && $adminMode);

// Номер «поколения» страниц: режим админа переключили — сервер теперь отдаёт
// (или прячет) скрытые релизы, кеши страниц сбрасываются, а App.svelte
// пересоздаёт текущую страницу ({#key}), и она грузится заново.
export const pageEpoch = writable(0);
let lastCanEdit = null;
canEdit.subscribe((on) => {
  if (lastCanEdit !== null && on !== lastCanEdit) {
    clearPageCaches();
    pageEpoch.update((n) => n + 1);
  }
  lastCanEdit = on;
});
