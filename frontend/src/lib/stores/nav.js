import { writable } from 'svelte/store';
import { parseRoute, paths } from '../route.js';

export { paths };

export const navStore = writable(parseRoute(location.pathname, location.search));

const scrollers = new Set();
let cameFromHistory = false;

export const fromHistory = () => cameFromHistory;

if (history.state?.overlay) {
  const { overlay: _, ...rest } = history.state;
  history.replaceState(rest, '');
}

const entryPos = () => history.state?.pos || 0;
let pos = entryPos();
function pushEntry(state, path) {
  history.pushState({ ...state, pos: pos + 1 }, '', path);
  pos = entryPos();
}

const address = () => location.pathname + location.search;
let shownAddress = address();
function showPage() {
  shownAddress = address();
  navStore.set(parseRoute(location.pathname, location.search));
}

const overlays = [];
let skipPops = 0;
let replaceAfterBack = null;

const reopeners = new Map();
export function reopensOnForward(kind, reopen) {
  reopeners.set(kind, reopen);
  return () => {
    if (reopeners.get(kind) === reopen) reopeners.delete(kind);
  };
}

export const reopening = (kind) => history.state?.overlay === kind && !overlays.some((o) => o.pos === pos);

export function backCloses(close, kind = 'window') {
  const entry = { close, pos: null };
  overlays.push(entry);
  settled(() => {
    if (!overlays.includes(entry)) return;
    if (!reopening(kind)) pushEntry({ ...history.state, overlay: kind });
    entry.pos = pos;
  });
  return () => {
    const i = overlays.indexOf(entry);
    if (i < 0) return;
    overlays.splice(i, 1);
    if (entry.pos && history.state?.overlay && pos >= entry.pos) popOverlayEntries(1);
  };
}

function closeOverlaysAbove(limit) {
  const before = overlays.length;
  while (overlays.length && overlays[overlays.length - 1].pos > limit) overlays.pop().close();
  return overlays.length < before;
}

let heldBack = null;
export function holdBack(close) {
  heldBack = close;
  return () => {
    if (heldBack === close) heldBack = null;
  };
}

function returnTo(from) {
  skipPops++;
  history.go(from - pos || 1);
}

let pendingPops = 0;
function popOverlayEntries(n) {
  if (!pendingPops) {
    queueMicrotask(() => {
      skipPops++;
      history.go(-pendingPops);
      pendingPops = 0;
    });
  }
  pendingPops += n;
}

const waiting = [];
let giveUpWaiting = null;
const historyBusy = () => pendingPops > 0 || skipPops > 0;
function settled(fn) {
  if (!historyBusy()) return fn();
  waiting.push(fn);
  giveUpWaiting ??= setTimeout(() => {
    skipPops = 0;
    runWaiting();
  }, 1000);
}
function runWaiting() {
  while (waiting.length && !historyBusy()) waiting.shift()();
  if (!waiting.length) {
    clearTimeout(giveUpWaiting);
    giveUpWaiting = null;
  }
}

let leaveGuard = null;
export function guardLeave(fn) {
  leaveGuard = fn;
  return () => {
    if (leaveGuard === fn) leaveGuard = null;
  };
}
const mayLeave = () => !leaveGuard || leaveGuard();

window.addEventListener('popstate', () => {
  const from = pos;
  pos = entryPos();
  if (replaceAfterBack) {
    history.replaceState({ pos }, '', replaceAfterBack);
    replaceAfterBack = null;
    cameFromHistory = false;
    showPage();
    return;
  }
  if (skipPops) {
    skipPops--;
    runWaiting();
    return;
  }
  const forward = pos > from;
  if (heldBack) {
    heldBack();
    heldBack = null;
    if (!forward) {
      returnTo(from);
      return;
    }
  }
  if (forward && history.state?.overlay && address() === shownAddress) {
    if (!reopeners.get(history.state.overlay)?.()) returnTo(from);
    return;
  }
  if (closeOverlaysAbove(pos) && address() === shownAddress) return;
  if (!mayLeave()) {
    returnTo(from);
    return;
  }
  for (const s of scrollers) s.cancel();
  cameFromHistory = true;
  showPage();
});

export function navigate(path, { replace = false } = {}) {
  if (historyBusy()) return settled(() => navigate(path, { replace }));
  if (path === address()) return;
  if (!mayLeave()) return;
  for (const s of scrollers) s.flush();
  cameFromHistory = false;
  if (history.state?.overlay) {
    overlays.length = 0;
    if (replace) {
      replaceAfterBack = path;
      history.back();
      return;
    }
    replace = true;
  }
  if (replace) history.replaceState({ scroll: history.state?.scroll || 0, pos }, '', path);
  else pushEntry({}, path);
  showPage();
}

export function navigateAfterPaint(path) {
  requestAnimationFrame(() => requestAnimationFrame(() => navigate(path)));
}

export const lastSearch = writable('');

export function scrollMemory(node) {
  let observer = null;
  let giveUp = null;
  let target = 0;

  function stopRestoring() {
    observer?.disconnect();
    observer = null;
    clearTimeout(giveUp);
  }
  function tryRestore() {
    if (node.scrollHeight - node.clientHeight >= target) {
      node.scrollTop = target;
      stopRestoring();
    }
  }
  function onRoute() {
    stopRestoring();
    target = cameFromHistory ? history.state?.scroll || 0 : 0;
    node.scrollTop = 0;
    if (target > 0) {
      observer = new MutationObserver(tryRestore);
      observer.observe(node, { childList: true, subtree: true });
      giveUp = setTimeout(stopRestoring, 2000);
      tryRestore();
    }
  }

  let saveHandle;
  const saver = {
    flush() {
      clearTimeout(saveHandle);
      history.replaceState({ ...history.state, scroll: node.scrollTop }, '');
    },
    cancel() {
      clearTimeout(saveHandle);
    },
  };
  function onScroll() {
    if (observer) return;
    clearTimeout(saveHandle);
    saveHandle = setTimeout(saver.flush, 150);
  }
  node.addEventListener('scroll', onScroll, { passive: true });
  scrollers.add(saver);

  let first = true;
  const unsubscribe = navStore.subscribe(() => {
    if (first) {
      first = false;
      target = history.state?.scroll || 0;
      if (target > 0) {
        observer = new MutationObserver(tryRestore);
        observer.observe(node, { childList: true, subtree: true });
        giveUp = setTimeout(stopRestoring, 2000);
      }
      return;
    }
    onRoute();
  });

  return {
    destroy() {
      unsubscribe();
      stopRestoring();
      saver.cancel();
      scrollers.delete(saver);
      node.removeEventListener('scroll', onScroll);
    },
  };
}

export function link(node) {
  function onClick(e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(node.getAttribute('href'));
  }
  node.addEventListener('click', onClick);
  return {
    destroy() {
      node.removeEventListener('click', onClick);
    },
  };
}
