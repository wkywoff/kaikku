import { AXIS_LOCK, EDGE } from './swipe.js';

const SHARE = 0.3;
const FLICK = 0.5;

export function decideDismiss({ dy, dt, height }) {
  if (dy <= 0) return false;
  return dy > height * SHARE || (dy > 30 && dy / Math.max(1, dt) > FLICK);
}

export function dragDismiss(node, opts) {
  let start = null;
  let dy = 0;
  let frame = 0;
  const apply = () => {
    frame = 0;
    node.style.transform = dy ? `translate3d(0, ${dy}px, 0)` : '';
    opts.onDrag?.(dy, node.offsetHeight);
  };
  const onStart = (e) => {
    if (e.touches.length !== 1) return;
    if (e.touches[0].clientX < EDGE) return;
    const list = opts.scroller?.();
    if (list && list.scrollTop > 0 && e.target instanceof Node && list.contains(e.target)) return;
    start = { x: e.touches[0].clientX, y: e.touches[0].clientY, t: e.timeStamp, axis: null };
    dy = 0;
  };
  const onMove = (e) => {
    if (!start) return;
    const t = e.touches[0];
    if (!start.axis) {
      const ax = Math.abs(t.clientX - start.x);
      const ay = Math.abs(t.clientY - start.y);
      if (Math.max(ax, ay) < AXIS_LOCK) return;
      if (ax > ay || t.clientY < start.y) {
        start = null;
        return;
      }
      start.axis = 'y';
      node.style.transition = 'none';
    }
    e.preventDefault();
    dy = Math.max(0, t.clientY - start.y);
    if (!frame) frame = requestAnimationFrame(apply);
  };
  const onEnd = (e) => {
    if (!start) return;
    const dragged = start.axis === 'y';
    const dt = e.timeStamp - start.t;
    start = null;
    if (!dragged) return;
    cancelAnimationFrame(frame);
    frame = 0;
    if (e.type !== 'touchcancel' && decideDismiss({ dy, dt, height: node.offsetHeight || 1 })) {
      apply();
      opts.onDismiss?.();
      return;
    }
    dy = 0;
    node.style.transition = 'transform 0.2s ease-out';
    apply();
  };
  node.addEventListener('touchstart', onStart, { passive: true });
  node.addEventListener('touchmove', onMove, { passive: false });
  node.addEventListener('touchend', onEnd);
  node.addEventListener('touchcancel', onEnd);
  return {
    update(next) {
      opts = next;
    },
    destroy() {
      cancelAnimationFrame(frame);
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', onEnd);
      node.removeEventListener('touchcancel', onEnd);
    },
  };
}

const BACK_AT = 40;
const TAP_SLOP = 10;

export function isBackSwipe(dx, dy) {
  return dx > BACK_AT && dx > Math.abs(dy);
}

export function startsSideways(dx, dy) {
  return Math.abs(dx) >= Math.abs(dy);
}

export function edgeBack(node, onBack) {
  let start = null;
  const onStart = (e) => {
    start = null;
    if (e.touches.length !== 1) return;
    const { clientX: x, clientY: y } = e.touches[0];
    const left = x < EDGE;
    if (!left && x <= window.innerWidth - EDGE) return;
    if (e.cancelable) e.preventDefault();
    start = { x, y, left, moved: false };
  };
  const onMove = (e) => {
    if (!start) return;
    const dx = e.touches[0].clientX - start.x;
    const dy = e.touches[0].clientY - start.y;
    if (!start.moved) {
      if (Math.hypot(dx, dy) <= TAP_SLOP) return;
      start.moved = true;
      if (!startsSideways(dx, dy)) start.left = false;
    }
    if (start.left && isBackSwipe(dx, dy)) {
      start = null;
      onBack?.();
    }
  };
  const onEnd = (e) => {
    const s = start;
    start = null;
    if (!s || s.moved || e.type === 'touchcancel') return;
    const el = document.elementFromPoint(s.x, s.y);
    if (el && node.contains(el)) (el.closest('button, a') || el).click?.();
  };
  node.addEventListener('touchstart', onStart, { passive: false });
  node.addEventListener('touchmove', onMove, { passive: true });
  node.addEventListener('touchend', onEnd);
  node.addEventListener('touchcancel', onEnd);
  return {
    update(next) {
      onBack = next;
    },
    destroy() {
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', onEnd);
      node.removeEventListener('touchcancel', onEnd);
    },
  };
}
