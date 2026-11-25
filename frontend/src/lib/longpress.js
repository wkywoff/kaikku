export const LONG_PRESS_MS = 500;
export const LONG_PRESS_SLOP = 10;

export function createLongPress(fire, { delay = LONG_PRESS_MS, slop = LONG_PRESS_SLOP, timers = globalThis } = {}) {
  let timer = null;
  let start = null;
  const cancel = () => {
    if (timer !== null) timers.clearTimeout(timer);
    timer = null;
    start = null;
  };
  return {
    down(x, y) {
      cancel();
      start = { x, y };
      timer = timers.setTimeout(() => {
        timer = null;
        const at = start;
        start = null;
        fire(at);
      }, delay);
    },
    move(x, y) {
      if (start && Math.hypot(x - start.x, y - start.y) > slop) cancel();
    },
    cancel,
  };
}

export function longpress(node, handler) {
  let fired = false;
  const press = createLongPress(({ x, y }) => {
    fired = true;
    navigator.vibrate?.(10);
    handler({ type: 'longpress', clientX: x, clientY: y, preventDefault() {}, stopPropagation() {} });
  });
  const onDown = (e) => {
    if (e.pointerType === 'mouse' || e.button > 0) return;
    fired = false;
    press.down(e.clientX, e.clientY);
  };
  const onMove = (e) => press.move(e.clientX, e.clientY);
  const onClick = (e) => {
    if (!fired) return;
    fired = false;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  const cancel = () => press.cancel();
  node.addEventListener('pointerdown', onDown);
  node.addEventListener('pointermove', onMove);
  node.addEventListener('pointerup', cancel);
  node.addEventListener('pointercancel', cancel);
  node.addEventListener('contextmenu', cancel);
  node.addEventListener('click', onClick, true);
  return {
    update(h) {
      handler = h;
    },
    destroy() {
      press.cancel();
      node.removeEventListener('pointerdown', onDown);
      node.removeEventListener('pointermove', onMove);
      node.removeEventListener('pointerup', cancel);
      node.removeEventListener('pointercancel', cancel);
      node.removeEventListener('contextmenu', cancel);
      node.removeEventListener('click', onClick, true);
    },
  };
}
