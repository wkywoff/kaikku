import { backCloses } from './stores/nav.js';
import { focusTrap } from './focusTrap.js';

const open = {};

const options = (arg) => (typeof arg === 'function' ? { close: arg } : arg || {});

export function modal(node, arg) {
  let { close: onBack, keepPlayer } = options(arg);
  const cls = keepPlayer ? 'modal-keep-player' : 'modal-open';
  open[cls] = (open[cls] || 0) + 1;
  document.body.classList.add(cls);
  const closed = backCloses(() => onBack?.());
  const trap = focusTrap(node);
  return {
    update(next) {
      onBack = options(next).close;
    },
    destroy() {
      trap.destroy();
      closed();
      if (--open[cls] === 0) document.body.classList.remove(cls);
    },
  };
}
