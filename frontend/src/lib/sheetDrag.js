// Шторку снизу (меню на телефоне, MenuSheet) смахивают вниз, как в Spotify:
// она едет за пальцем, отпустили далеко или резко — закрывается, иначе
// возвращается на место.
import { AXIS_LOCK, EDGE } from './swipe.js';

const SHARE = 0.3; // доля высоты шторки, после которой отпускание закрывает
const FLICK = 0.5; // px/мс: быстрый короткий взмах закрывает и раньше

/** Закрыть ли шторку, когда палец отпустили: dy — сдвиг вниз, dt — мс. */
export function decideDismiss({ dy, dt, height }) {
  if (dy <= 0) return false;
  return dy > height * SHARE || (dy > 30 && dy / Math.max(1, dt) > FLICK);
}

/**
 * Svelte-действие: use:dragDismiss={{ scroller, onDrag, onDismiss }}.
 *   scroller() — прокручиваемый список внутри шторки: пока он прокручен
 *   вниз, палец листает его, а шторка стоит;
 *   onDrag(dy, height) — шторку сдвинули на dy (0 — вернулась);
 *   onDismiss() — закрыть. Сдвиг при этом остаётся: шторка уезжает с того
 *   места, где её отпустили (MenuSheet, out:drop).
 */
export function dragDismiss(node, opts) {
  let start = null; // { x, y, t, axis }
  let dy = 0;
  let frame = 0;
  const apply = () => {
    frame = 0;
    node.style.transform = dy ? `translate3d(0, ${dy}px, 0)` : '';
    opts.onDrag?.(dy, node.offsetHeight);
  };
  const onStart = (e) => {
    if (e.touches.length !== 1) return;
    // От левого края — жест «назад» (edgeBack ниже), а не свайп вниз.
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
      // Вбок или вверх — не наш жест: вверх листается список.
      if (ax > ay || t.clientY < start.y) {
        start = null;
        return;
      }
      start.axis = 'y';
      node.style.transition = 'none';
    }
    e.preventDefault(); // не прокрутка и не обновление страницы
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

const BACK_AT = 40; // px вправо от левого края — это «назад»
const TAP_SLOP = 10; // px: дальше — уже не нажатие

/** Свайп от левого края — «назад»: палец ушёл вправо дальше, чем вбок по вертикали. */
export function isBackSwipe(dx, dy) {
  return dx > BACK_AT && dx > Math.abs(dy);
}

/**
 * Svelte-действие для слоя во весь экран (шторка меню): use:edgeBack={close}.
 * Свайп от края экрана на iPhone — системный «назад»: он увёл бы страницу
 * вместе со шторкой вбок, а под ней показал бы снимок той же страницы.
 * Касание у края забираем себе (preventDefault на touchstart системный жест
 * отменяет), и свайп от левого края закрывает шторку как обычно, на месте.
 * Забранное касание не даёт click — короткое нажатие у края превращаем в
 * него сами, иначе у края не нажимались бы пункты.
 */
export function edgeBack(node, onBack) {
  let start = null; // { x, y, left, moved }
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
    if (Math.hypot(dx, dy) > TAP_SLOP) start.moved = true;
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
