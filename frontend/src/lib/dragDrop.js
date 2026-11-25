export function rowStep(slot) {
  return slot.offsetHeight + (parseFloat(getComputedStyle(slot.parentElement).rowGap) || 0);
}

export const dropRest = (drag, to) => drag.dy - (to - drag.i) * drag.h;

export const nextPaint = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
