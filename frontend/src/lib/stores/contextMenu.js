import { writable } from 'svelte/store';

// Одно контекстное меню на всё приложение (components/ContextMenu.svelte).
// items: [{ label, icon?, onClick?, href?, submenu?: items, danger?, checked?, separator?, heading? }]
//   checked — выбранный вариант (зелёный, с галочкой); heading — подпись группы, не кликается.
export const contextMenu = writable(null); // { x, y, items }

/** Открыть меню у курсора (правый клик) или под элементом (кнопка «…»). */
export function openMenu(event, items) {
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
  contextMenu.set({ x, y, items });
}

export function closeMenu() {
  contextMenu.set(null);
}
