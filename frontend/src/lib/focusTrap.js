const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]';

export function cycleTarget(items, current, back) {
  if (!items.length) return null;
  const i = items.indexOf(current);
  if (i === -1) return back ? items[items.length - 1] : items[0];
  if (back && i === 0) return items[items.length - 1];
  if (!back && i === items.length - 1) return items[0];
  return null;
}

export function focusTrap(node) {
  const before = document.activeElement;
  if (!node.hasAttribute('tabindex')) node.setAttribute('tabindex', '-1');

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
      const active = document.activeElement;
      if (before instanceof HTMLElement && before.isConnected && (!active || active === document.body || node.contains(active))) {
        before.focus({ preventScroll: true });
      }
    },
  };
}
