import { readable } from 'svelte/store';

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
