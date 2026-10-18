import { get } from 'svelte/store';
import { player } from './stores/player.js';

// Колёсико мыши над громкостью: щелчок колеса — 5 %, мелкие шаги тачпада —
// пропорционально. Svelte-action: <div use:wheelVolume>. Слушатель не
// пассивный — иначе страница под плеером прокручивалась бы вместе с ним.
const NOTCH_PX = 40; // меньше — это тачпад, а не щелчок колеса
const PX_PER_NOTCH = 100; // браузер может прислать несколько щелчков одним событием
const STEP = 0.05;

export function wheelStep(e) {
  const px = e.deltaMode === 1 ? e.deltaY * 33 : e.deltaMode === 2 ? e.deltaY * 400 : e.deltaY;
  if (!px) return 0;
  const abs = Math.abs(px);
  const step = abs >= NOTCH_PX ? Math.min(5, Math.max(1, Math.round(abs / PX_PER_NOTCH))) * STEP : (abs / NOTCH_PX) * STEP;
  return px < 0 ? step : -step; // от себя — громче
}

export function wheelVolume(node) {
  const onWheel = (e) => {
    const step = wheelStep(e);
    if (!step) return;
    e.preventDefault();
    player.setVolume(Math.round((get(player).volume + step) * 1000) / 1000);
  };
  node.addEventListener('wheel', onWheel, { passive: false });
  return { destroy: () => node.removeEventListener('wheel', onWheel) };
}
