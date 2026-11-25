import { writable } from 'svelte/store';

const KEY = 'kaikku_lyrics_open';

function load() {
  try {
    return localStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
}

export const lyricsOpen = writable(load());
lyricsOpen.subscribe((on) => {
  try {
    localStorage.setItem(KEY, on ? '1' : '0');
  } catch {
  }
});
