// Системный плеер (Media Session): экран блокировки телефона, кнопки на
// наушниках и клавиатуре, виджет в KDE/GNOME. Где его нет — ничего не делает.

import { artworkFor } from '../../imageproxy.js';

const supported = () => 'mediaSession' in navigator;

// Кнопка на экране блокировки (iOS, Android) — по тому, что на самом деле:
// без этого после паузы в приложении она так и показывала «пауза».
export function setPlaybackState(value) {
  if (supported()) navigator.mediaSession.playbackState = value;
}

/** Показать трек; controls — { play, pause, prev, next, seek(секунды) }. */
export function showTrack(track, controls) {
  if (!supported()) return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: track.title,
    artist: track.artist || '',
    album: track.album || '',
    artwork: artworkFor(track.coverUrl),
  });
  navigator.mediaSession.setActionHandler('play', () => controls.play());
  navigator.mediaSession.setActionHandler('pause', () => controls.pause());
  navigator.mediaSession.setActionHandler('previoustrack', () => controls.prev());
  navigator.mediaSession.setActionHandler('nexttrack', () => controls.next());
  navigator.mediaSession.setActionHandler('seekto', (d) => controls.seek(d.seekTime));
}

/** Позиция и длина трека по играющему элементу. */
export function updatePositionState(el) {
  if (!supported() || !navigator.mediaSession.setPositionState) return;
  if (!Number.isFinite(el.duration) || el.duration <= 0) return;
  try {
    navigator.mediaSession.setPositionState({
      duration: el.duration,
      position: Math.min(el.currentTime, el.duration),
      playbackRate: 1,
    });
  } catch {
    /* некоторые браузеры кидают на граничных значениях — не критично */
  }
}

/** Ничего не играет (выход из аккаунта). */
export function clearTrack() {
  if (supported()) navigator.mediaSession.metadata = null;
  setPlaybackState('none');
}
