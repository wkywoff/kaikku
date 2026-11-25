import { writable } from 'svelte/store';
import { backCloses, holdBack } from './nav.js';
import { appleTouch } from '../platform.js';

export const contextMenu = writable(null);

const PHONE = '(max-width: 768px)';

let release = null;
let byBack = false;
export const closedByBack = () => byBack;
function stopWatchingBack() {
  release?.();
  release = null;
}
function onBack() {
  byBack = true;
  closeMenu();
}
function watchBack() {
  if (appleTouch) return holdBack(onBack);
  if (typeof CloseWatcher === 'function') {
    try {
      const watcher = new CloseWatcher();
      watcher.onclose = onBack;
      return () => watcher.destroy();
    } catch {
    }
  }
  return backCloses(onBack);
}

export function openMenu(event, items, header = null) {
  event.preventDefault();
  event.stopPropagation();
  let x = event.clientX;
  let y = event.clientY;
  if (event.type !== 'contextmenu' && event.currentTarget?.getBoundingClientRect) {
    const r = event.currentTarget.getBoundingClientRect();
    x = r.right;
    y = r.bottom + 4;
  }
  const sheet = typeof matchMedia === 'function' && matchMedia(PHONE).matches;
  if (!sheet) stopWatchingBack();
  else release ??= watchBack();
  byBack = false;
  contextMenu.set({ x, y, items, header, sheet });
}

export function closeMenu() {
  stopWatchingBack();
  contextMenu.set(null);
}
