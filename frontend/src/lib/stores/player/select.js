import { readable } from 'svelte/store';

/**
 * Часть состояния плеера, которая меняется редко. Сам плеер меняется
 * 4 раза в секунду (timeupdate — позиция трека), и всё, что подписано на
 * него целиком, пересчитывается на каждом тике. select выдаёт новое
 * значение (fn), только когда изменилось что-то из deps.
 */
export function select(store, deps, fn = (s) => deps(s)[0]) {
  return readable(undefined, (set) => {
    let prev = null;
    return store.subscribe((s) => {
      const d = deps(s);
      if (prev && d.every((x, i) => x === prev[i])) return;
      prev = d;
      set(fn(s));
    });
  });
}
