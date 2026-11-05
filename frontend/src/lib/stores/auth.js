import { writable } from 'svelte/store';

const STORAGE_KEY = 'kaikku_token';

function subjectOf(token) {
  try {
    return JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/'))).sub;
  } catch {
    return null;
  }
}

function createAuthStore() {
  const { subscribe, set } = writable(localStorage.getItem(STORAGE_KEY));
  let current = localStorage.getItem(STORAGE_KEY);
  const apply = (token) => {
    current = token;
    set(token);
  };

  // Вход общий на все вкладки: вышли или вошли другим в одной — остальные
  // следуют за ней (выход перезагрузит страницу в App.svelte, смена человека — здесь).
  globalThis.addEventListener?.('storage', (e) => {
    if (e.key !== STORAGE_KEY && e.key !== null) return;
    const next = localStorage.getItem(STORAGE_KEY);
    const switched = next && current && subjectOf(next) !== subjectOf(current);
    apply(next);
    if (switched) location.reload();
  });

  return {
    subscribe,
    login(token) {
      localStorage.setItem(STORAGE_KEY, token);
      apply(token);
    },
    // Продление: только если общий токен всё ещё тот, что продлевали. Иначе
    // устаревшая вкладка вернула бы вход тому, кто уже вышел.
    renew(old, fresh) {
      if (current !== old) return;
      if (localStorage.getItem(STORAGE_KEY) !== old) return;
      localStorage.setItem(STORAGE_KEY, fresh);
      apply(fresh);
    },
    logout() {
      localStorage.removeItem(STORAGE_KEY);
      apply(null);
    },
  };
}

export const auth = createAuthStore();
