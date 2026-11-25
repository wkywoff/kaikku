import { readable } from 'svelte/store';

export function createSmoothTime(state, position) {
  return readable(0, (set) => {
    let frame = 0;
    let last = 0;
    const tick = () => {
      let time = position();
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
