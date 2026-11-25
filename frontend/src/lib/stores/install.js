import { writable } from 'svelte/store';

export const installPrompt = writable(null);

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault();
    installPrompt.set(e);
  });
  window.addEventListener('appinstalled', () => installPrompt.set(null));
}

export async function install(prompt) {
  if (!prompt) return;
  prompt.prompt();
  await prompt.userChoice.catch(() => null);
  installPrompt.set(null);
}
