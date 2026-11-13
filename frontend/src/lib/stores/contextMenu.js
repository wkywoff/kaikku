import { writable } from 'svelte/store';
import { backCloses } from './nav.js';

// Одно контекстное меню на всё приложение (components/ContextMenu.svelte).
// items: [{ label, icon?, onClick?, href?, submenu?: items, danger?, checked?, separator?, heading? }]
//   checked — выбранный вариант (зелёный, с галочкой); heading — подпись группы, не кликается;
//   image — круглая картинка вместо значка (фото исполнителя); null — фото нет, заглушка.
// header: { title, subtitle?, image?, round? } — о чём меню (трек, альбом,
//   артист). Показывается только на телефоне: там меню — шторка снизу
//   (components/MenuSheet.svelte), и строки, по которой нажали, под ней не видно.
export const contextMenu = writable(null); // { x, y, items, header, sheet }

// Телефон — та же граница, что у остальной вёрстки (нижняя панель и т.п.).
const PHONE = '(max-width: 768px)';

// Шторка закрывается и жестом «назад» (как очередь и окна). Запись в
// истории снимается здесь же, в closeMenu, а не когда шторка уедет с
// экрана: пункт меню может сразу открыть окно со своей записью.
let release = null;
// Меню закрыли жестом «назад» (а не нажатием): MenuSheet на iPhone тогда
// убирает шторку без анимации.
let byBack = false;
export const closedByBack = () => byBack;
function dropHistoryEntry() {
  release?.();
  release = null;
}

/** Открыть меню у курсора (правый клик) или под элементом (кнопка «…»). */
export function openMenu(event, items, header = null) {
  event.preventDefault();
  event.stopPropagation();
  let x = event.clientX;
  let y = event.clientY;
  // С клавиатуры (Enter на «…») координат курсора нет — берём кнопку.
  if (event.type !== 'contextmenu' && event.currentTarget?.getBoundingClientRect) {
    const r = event.currentTarget.getBoundingClientRect();
    x = r.right;
    y = r.bottom + 4;
  }
  const sheet = typeof matchMedia === 'function' && matchMedia(PHONE).matches;
  if (!sheet) dropHistoryEntry();
  else {
    release ??= backCloses(() => {
      byBack = true;
      closeMenu();
    });
  }
  byBack = false;
  contextMenu.set({ x, y, items, header, sheet });
}

export function closeMenu() {
  dropHistoryEntry();
  contextMenu.set(null);
}
