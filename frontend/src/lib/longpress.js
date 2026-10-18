// Долгое нажатие пальцем — меню, как правый клик мышью. На iPhone Safari
// не присылает contextmenu на долгое нажатие, поэтому без этого меню трека
// открывалось только кнопкой «…». Мышь не трогаем: у неё есть правый клик.

export const LONG_PRESS_MS = 500;
export const LONG_PRESS_SLOP = 10; // px: дальше — это прокрутка, а не нажатие

/**
 * Чистый автомат долгого нажатия (тестируется без DOM). fire({x, y}) —
 * когда палец продержался delay мс и почти не сдвинулся.
 */
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

/**
 * Svelte-действие: use:longpress={(event) => openMenu(event, items)}.
 * event похож на событие мыши: clientX/clientY, preventDefault и
 * stopPropagation (openMenu вызывает их). Клик, который палец сделает,
 * отпустив экран, после меню не считается — иначе трек ещё и включился бы.
 */
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
  node.addEventListener('pointercancel', cancel); // браузер начал прокрутку
  node.addEventListener('contextmenu', cancel); // Android сам открыл меню
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
