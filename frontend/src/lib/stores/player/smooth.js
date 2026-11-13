// Позиция для полосы прогресса: пока трек играет — на каждом кадре, иначе
// полоса двигалась бы рывками 4 раза в секунду (timeupdate). Кадры идут,
// только пока на неё кто-то подписан и вкладка видна.

import { readable } from 'svelte/store';

/** state — стор плеера; position() — точная позиция играющего трека. */
export function createSmoothTime(state, position) {
  return readable(0, (set) => {
    let frame = 0;
    let last = 0;
    const tick = () => {
      let time = position();
      // currentTime в браузере иногда чуть отступает назад — полоса не должна дрожать.
      if (time < last && last - time < 0.25) time = last;
      last = time;
      set(time);
      frame = requestAnimationFrame(tick);
    };
    const unsubscribe = state.subscribe((s) => {
      if (s.isPlaying && typeof requestAnimationFrame === 'function') {
        if (!frame) frame = requestAnimationFrame(tick);
        return;
      }
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
      last = s.currentTime;
      set(s.currentTime);
    });
    return () => {
      unsubscribe();
      if (frame) cancelAnimationFrame(frame);
    };
  });
}
