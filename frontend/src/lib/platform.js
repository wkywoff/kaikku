// iPhone и iPad (и iPad, который в Safari выдаёт себя за Mac): у них свои
// ограничения — звук в фоне (stores/player.js), жест «назад» от края экрана
// (NowPlaying).
export const appleTouch =
  typeof navigator !== 'undefined' &&
  (/iPhone|iPad|iPod/.test(navigator.userAgent || '') || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1));
