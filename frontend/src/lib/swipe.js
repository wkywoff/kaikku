// Свайп влево/вправо по мини-плееру и обложке в полноэкранном плеере —
// следующий/предыдущий трек, как в Spotify на телефоне.

export const AXIS_LOCK = 8; // px: до этого сдвига направление ещё не решено
export const EDGE = 24; // px от левого края: там жест iPhone «назад», не наш
const SHARE = 0.3; // доля ширины, после которой отпускание переключает трек
const FLICK = 0.5; // px/мс: быстрый короткий взмах переключает и раньше

/**
 * Что сделать, когда палец отпустили: 'next' (повели влево), 'prev'
 * (вправо) или null — вернуть на место.
 */
export function decideSwipe({ dx, dt, width }) {
  const far = Math.abs(dx) > width * SHARE;
  const flick = Math.abs(dx) > 30 && Math.abs(dx) / Math.max(1, dt) > FLICK;
  if (!far && !flick) return null;
  return dx < 0 ? 'next' : 'prev';
}

/**
 * Svelte-действие: use:swipeTrack={{ onNext, onPrev, canNext, canPrev, slide }}.
 * Элемент едет за пальцем (раз за кадр), отпустили далеко или резко —
 * уезжает и вызывает обработчик, иначе возвращается. Вертикальное движение
 * не трогает (там прокрутка или свайп вниз, закрывающий плеер). Клик,
 * которым кончается свайп, не считается — иначе мини-плеер ещё и раскрывался бы.
 *   canNext/canPrev: false — туда некуда: элемент тянется туго и возвращается;
 *   slide: N — рядом лежат соседние обложки через N px (класс swiping на
 *   время жеста их показывает): элемент доезжает до соседней целиком, и она
 *   встаёт на место, как в Spotify; без slide — уезжает и гаснет.
 */
export function swipeTrack(node, handlers) {
  let start = null; // { x, y, t, axis }
  let dx = 0;
  let frame = 0;
  let swallowClick = false;
  const apply = () => {
    frame = 0;
    node.style.transform = dx ? `translate3d(${dx}px, 0, 0)` : '';
  };
  const reset = (animate) => {
    cancelAnimationFrame(frame);
    frame = 0;
    node.style.transition = animate ? 'transform 0.2s ease-out' : '';
    node.style.transform = '';
    if (animate) setTimeout(() => node.classList.remove('swiping'), 200);
    else node.classList.remove('swiping');
  };
  // Туда, где нет трека, — туго, в четверть хода пальца.
  const blocked = (d) => (d < 0 && handlers.canNext === false) || (d > 0 && handlers.canPrev === false);
  const onStart = (e) => {
    if (e.touches.length !== 1) return;
    const t = e.touches[0];
    if (t.clientX < EDGE) return;
    if (e.target instanceof Element && e.target.closest('input, .slider, [role=slider]')) return;
    start = { x: t.clientX, y: t.clientY, t: e.timeStamp, axis: null };
    dx = 0;
    node.style.transition = 'none';
  };
  const onMove = (e) => {
    if (!start) return;
    const t = e.touches[0];
    if (!start.axis) {
      const ax = Math.abs(t.clientX - start.x);
      const ay = Math.abs(t.clientY - start.y);
      if (Math.max(ax, ay) < AXIS_LOCK) return;
      start.axis = ax > ay ? 'x' : 'y';
      if (start.axis === 'y') {
        start = null;
        return;
      }
      node.classList.add('swiping');
    }
    e.preventDefault();
    dx = t.clientX - start.x;
    if (blocked(dx)) dx *= 0.25;
    if (!frame) frame = requestAnimationFrame(apply);
  };
  const onEnd = (e) => {
    if (!start) return;
    const moved = start.axis === 'x';
    let action = moved ? decideSwipe({ dx, dt: e.timeStamp - start.t, width: node.offsetWidth || 1 }) : null;
    if (action && blocked(dx)) action = null;
    start = null;
    if (moved) swallowClick = true;
    if (!action) {
      dx = 0;
      reset(true);
      return;
    }
    cancelAnimationFrame(frame);
    frame = 0;
    const run = () => (action === 'next' ? handlers.onNext : handlers.onPrev)?.();
    if (handlers.slide !== undefined) {
      // Соседняя обложка доезжает на место; потом трек переключается, и на
      // месте элемента — та же картинка: прыжок назад без анимации не виден.
      const shift = (node.offsetWidth || 0) + handlers.slide;
      node.style.transition = 'transform 0.22s ease-out';
      node.style.transform = `translate3d(${action === 'next' ? -shift : shift}px, 0, 0)`;
      setTimeout(() => {
        run();
        dx = 0;
        requestAnimationFrame(() => {
          node.style.transition = 'none';
          node.style.transform = '';
          node.classList.remove('swiping');
        });
      }, 220);
      return;
    }
    node.style.transition = 'transform 0.18s ease-in, opacity 0.18s ease-in';
    node.style.transform = `translate3d(${action === 'next' ? '-' : ''}40%, 0, 0)`;
    node.style.opacity = '0';
    setTimeout(() => {
      run();
      dx = 0;
      node.classList.remove('swiping');
      node.style.transition = 'none';
      node.style.transform = '';
      requestAnimationFrame(() => {
        node.style.transition = 'opacity 0.18s ease-out';
        node.style.opacity = '';
      });
    }, 180);
  };
  const onClick = (e) => {
    if (!swallowClick) return;
    swallowClick = false;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  node.addEventListener('touchstart', onStart, { passive: true });
  node.addEventListener('touchmove', onMove, { passive: false });
  node.addEventListener('touchend', onEnd);
  node.addEventListener('touchcancel', onEnd);
  node.addEventListener('click', onClick, true);
  return {
    update(h) {
      handlers = h;
    },
    destroy() {
      cancelAnimationFrame(frame);
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', onEnd);
      node.removeEventListener('touchcancel', onEnd);
      node.removeEventListener('click', onClick, true);
    },
  };
}
