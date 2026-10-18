import { writable } from 'svelte/store';

const STORAGE_KEY = 'kaikku_token';

function createAuthStore() {
  const { subscribe, set } = writable(localStorage.getItem(STORAGE_KEY));

  return {
    subscribe,
    login(token) {
      localStorage.setItem(STORAGE_KEY, token);
      set(token);
    },
    logout() {
      localStorage.removeItem(STORAGE_KEY);
      set(null);
    },
  };
}

export const auth = createAuthStore();
