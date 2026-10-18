import { writable } from 'svelte/store';

// Показан ли текст в полноэкранном плеере (на этом устройстве): открыл
// текст однажды — и следующие песни открываются с ним же, как в Spotify.
// На компьютере закрытие плеера его сбрасывает (NowPlaying.svelte), на
// телефоне плеер и открывается снова с текстом.
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
    /* приватный режим — не запомнится */
  }
});
