import { writable } from 'svelte/store';

// Установка как приложения. Chrome/Edge/Android присылают
// beforeinstallprompt, когда сайт можно установить, — тогда в меню
// аккаунта появляется «Установить приложение». В Safari (iPhone) такого
// события нет: там «Поделиться» → «На экран „Домой“».
export const installPrompt = writable(null);

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault(); // своя кнопка вместо баннера браузера
    installPrompt.set(e);
  });
  window.addEventListener('appinstalled', () => installPrompt.set(null));
}

export async function install(prompt) {
  if (!prompt) return;
  prompt.prompt();
  await prompt.userChoice.catch(() => null);
  installPrompt.set(null); // запрос одноразовый
}
