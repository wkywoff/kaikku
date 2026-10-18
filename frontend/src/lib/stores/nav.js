import { writable } from 'svelte/store';
import { parseRoute, paths } from '../route.js';

export { paths };

// navStore — текущий маршрут (см. lib/route.js), синхронизирован с адресом
// страницы. Плеер живёт в App.svelte вне экранов и переживает навигацию.
// Называется navStore, не nav: иначе компилятор Svelte путает импорт с
// элементом <nav>.
export const navStore = writable(parseRoute(location.pathname, location.search));

// Активные прокручиваемые контейнеры со scrollMemory: перед уходом со
// страницы их позиция записывается в текущую запись истории.
const scrollers = new Set();
let cameFromHistory = false; // последний переход — «назад»/«вперёд»

/** Последний переход — «назад»/«вперёд» (кнопкой, жестом), а не по ссылке. */
export const fromHistory = () => cameFromHistory;

// Окна (lib/modal.js) закрываются жестом «назад», а не уводят со страницы:
// открытое окно кладёт в историю свою запись с тем же адресом; «назад»
// снимает её и закрывает окно. Закрыли окно сами (крестик, Esc) — запись
// убирается через history.back(), и на этот popstate навигация не реагирует.
// Перезагрузили страницу с открытым окном: окна закрыты, а запись осталась
// помеченной как запись окна — снять пометку.
if (history.state?.overlay) {
  const { overlay: _, ...rest } = history.state;
  history.replaceState(rest, '');
}

const overlays = []; // открытые окна, последнее — сверху: { id, close }
let overlaySeq = 0;
let skipPops = 0;
let replaceAfterBack = null; // адрес, которым заменить страницу под окном

/** Окно открылось: «назад» вызовет close. Вернёт функцию «окно закрылось». */
export function backCloses(close) {
  const entry = { id: ++overlaySeq, close };
  history.pushState({ ...history.state, overlay: entry.id }, '');
  overlays.push(entry);
  return () => {
    const i = overlays.indexOf(entry);
    if (i < 0) return; // уже закрыто «назад»
    overlays.splice(i, 1);
    // Всё ещё на записи окна (или на записи окна поверх него: id растут) —
    // снять её. Если уже ушли на другую страницу, navigate() эту запись
    // заменил, и overlay в состоянии нет.
    if (history.state?.overlay >= entry.id) popOverlayEntries(1);
  };
}

// Закрылись сразу несколько окон (плеер вместе с очередью в нём) — снять
// их записи одним переходом: несколько history.back() подряд браузеры
// выполняют ненадёжно.
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

// Страница, с которой нельзя просто уйти (окно загрузки с несохранённым
// или с идущей загрузкой): guardLeave(fn) — fn спрашивает человека и
// возвращает true, если уходить. Возвращает снятие защиты.
let leaveGuard = null;
export function guardLeave(fn) {
  leaveGuard = fn;
  return () => {
    if (leaveGuard === fn) leaveGuard = null;
  };
}
const mayLeave = () => !leaveGuard || leaveGuard();

window.addEventListener('popstate', () => {
  if (replaceAfterBack) {
    // Запись окна снята (navigate ниже) — теперь заменить саму страницу.
    history.replaceState({}, '', replaceAfterBack);
    replaceAfterBack = null;
    cameFromHistory = false;
    navStore.set(parseRoute(location.pathname, location.search));
    return;
  }
  if (skipPops) {
    skipPops--;
    return;
  }
  if (overlays.length) {
    overlays.pop().close();
    return;
  }
  if (!mayLeave()) {
    // «Назад» уже сменил адрес — вернуться вперёд, страница остаётся.
    skipPops++;
    history.go(1);
    return;
  }
  // История уже переключилась — отложенная запись позиции ушла бы в чужую запись.
  for (const s of scrollers) s.cancel();
  cameFromHistory = true;
  navStore.set(parseRoute(location.pathname, location.search));
});

// navigate открывает path. replace: true — заменить текущую запись истории
// (например, пока человек печатает поисковый запрос), а не плодить новую
// на каждую букву.
export function navigate(path, { replace = false } = {}) {
  if (path === location.pathname + location.search) return;
  if (!mayLeave()) return;
  for (const s of scrollers) s.flush();
  cameFromHistory = false;
  // Со страницы ушли при открытом окне: окно закроется вместе со страницей,
  // а его запись в истории не оставлять — иначе «назад» вело бы на ту же
  // страницу ещё раз. Обычный переход её заменяет. Переход «вместо этой
  // страницы» (удалили плейлист → на главную) сначала снимает её, потом
  // заменяет саму страницу (popstate выше) — иначе «назад» вело бы на
  // удалённый плейлист.
  if (history.state?.overlay) {
    overlays.length = 0;
    if (replace) {
      replaceAfterBack = path;
      history.back();
      return;
    }
    replace = true;
  }
  if (replace) history.replaceState({ scroll: history.state?.scroll || 0 }, '', path);
  else history.pushState({}, '', path);
  navStore.set(parseRoute(location.pathname, location.search));
}

// navigateAfterPaint — переход из меню: сначала меню исчезает с экрана,
// потом переход. Айфон снимает страницу для свайпа «назад» в момент
// перехода, и меню, закрытое в том же кадре, осталось бы на снимке.
export function navigateAfterPaint(path) {
  requestAnimationFrame(() => requestAnimationFrame(() => navigate(path)));
}

// Последний поисковый запрос: иконка поиска возвращает к нему.
export const lastSearch = writable('');

// scrollMemory — action для прокручиваемого контейнера страницы (main в
// App.svelte; он живёт всё время, меняется только содержимое). Позиция
// прокрутки хранится в записи истории: «назад»/«вперёд» возвращают туда же,
// новый переход открывает страницу сверху. Данные страница грузит
// асинхронно, поэтому позиция восстанавливается, как только содержимого
// хватает (но не дольше 2 с).
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
    if (observer) return; // ещё восстанавливаем — не перезаписывать позицию нулём
    clearTimeout(saveHandle);
    saveHandle = setTimeout(saver.flush, 150);
  }
  node.addEventListener('scroll', onScroll, { passive: true });
  scrollers.add(saver);

  let first = true;
  const unsubscribe = navStore.subscribe(() => {
    if (first) {
      first = false;
      target = history.state?.scroll || 0; // перезагрузка страницы
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

// link — action для <a href="/…">: обычный клик открывает страницу внутри
// приложения (без перезагрузки, плеер продолжает играть), а клик колёсиком,
// Ctrl/Cmd/Shift+клик и «открыть в новой вкладке» остаются браузеру.
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
