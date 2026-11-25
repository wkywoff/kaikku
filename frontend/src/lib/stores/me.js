import { writable, derived } from 'svelte/store';
import { clearPageCaches } from '../lru.js';

export const me = writable(null);
export const isAdmin = derived(me, ($me) => !!$me?.is_admin);

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
  }
});

export const canEdit = derived([isAdmin, adminMode], ([$isAdmin, $adminMode]) => $isAdmin && $adminMode);

export const pageEpoch = writable(0);
let lastCanEdit = null;
canEdit.subscribe((on) => {
  if (lastCanEdit !== null && on !== lastCanEdit) {
    clearPageCaches();
    pageEpoch.update((n) => n + 1);
  }
  lastCanEdit = on;
});
