import { get } from 'svelte/store';
import { player } from './stores/player.js';

const NOTCH_PX = 40;
const PX_PER_NOTCH = 100;
const STEP = 0.05;

export function wheelStep(e) {
  const px = e.deltaMode === 1 ? e.deltaY * 33 : e.deltaMode === 2 ? e.deltaY * 400 : e.deltaY;
  if (!px) return 0;
  const abs = Math.abs(px);
  const step = abs >= NOTCH_PX ? Math.min(5, Math.max(1, Math.round(abs / PX_PER_NOTCH))) * STEP : (abs / NOTCH_PX) * STEP;
  return px < 0 ? step : -step;
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
