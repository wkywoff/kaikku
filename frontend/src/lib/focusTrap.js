// Фокус внутри окна: Tab по кругу, после закрытия — обратно. Для окон через
// modal (modal.js) и тех, что живут вне страницы (полноэкранный плеер,
// сброс пароля).

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]';

// Куда перевести фокус по Tab (back — с Shift) из current, если дальше
// края списка: с последнего — на первый и наоборот, снаружи списка (само
// окно) — на край. null — браузер справится сам.
export function cycleTarget(items, current, back) {
  if (!items.length) return null;
  const i = items.indexOf(current);
  if (i === -1) return back ? items[items.length - 1] : items[0];
  if (back && i === 0) return items[items.length - 1];
  if (!back && i === items.length - 1) return items[0];
  return null;
}

// Фокус внутри окна. Без inert на остальной странице: меню трека и
// уведомления живут вне окна (App.svelte), и меню, открытое из окна, должно
// оставаться доступным. Ловушка — только пока фокус внутри окна.
//
//   <div role="dialog" use:focusTrap>…</div>
export function focusTrap(node) {
  const before = document.activeElement;
  if (!node.hasAttribute('tabindex')) node.setAttribute('tabindex', '-1');

  // Окно само ставит фокус (autofocus, кнопка «закрыть» в onMount) — после
  // этого действия; если не поставило, фокус — на само окно, чтобы Tab
  // начинался внутри него.
  const start = setTimeout(() => {
    if (!node.contains(document.activeElement)) node.focus({ preventScroll: true });
  });

  const onKey = (e) => {
    if (e.key !== 'Tab' || e.defaultPrevented) return;
    const items = [...node.querySelectorAll(FOCUSABLE)].filter((el) => el.getClientRects().length && !el.closest('[inert]'));
    const target = cycleTarget(items, document.activeElement, e.shiftKey);
    if (!items.length || target) {
      e.preventDefault();
      target?.focus();
    }
  };
  node.addEventListener('keydown', onKey);

  return {
    destroy() {
      clearTimeout(start);
      node.removeEventListener('keydown', onKey);
      // Вернуть фокус, только если он был в окне (или потерялся вместе с
      // ним): если человек уже ушёл фокусом в другое место, не дёргать.
      const active = document.activeElement;
      if (before instanceof HTMLElement && before.isConnected && (!active || active === document.body || node.contains(active))) {
        before.focus({ preventScroll: true });
      }
    },
  };
}
