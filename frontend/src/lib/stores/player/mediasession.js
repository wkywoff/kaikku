import { artworkFor } from '../../imageproxy.js';

const supported = () => 'mediaSession' in navigator;

export function setPlaybackState(value) {
  if (supported()) navigator.mediaSession.playbackState = value;
}

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
  }
}

export function clearTrack() {
  if (supported()) navigator.mediaSession.metadata = null;
  setPlaybackState('none');
}
