const pageCaches = new Set();

/** Забыть всё, что страницы закешировали (переключили режим админа — в
 * ответах сервера появились или пропали скрытые релизы). */
export function clearPageCaches() {
  for (const c of pageCaches) c.clear();
}

// Map с пределом размера для кешей страниц: прочитанная запись становится
// самой свежей, лишние — самые давние — удаляются. Приложение бывает
// открыто неделями, и без предела такие кеши только растут.
export class LRU extends Map {
  /**
   * @param {number} max
   * @param {{ pages?: boolean }} [opts] pages: false — не кеш страниц (его
   *   не сбрасывает clearPageCaches).
   */
  constructor(max, { pages = true } = {}) {
    super();
    this.max = max;
    if (pages) pageCaches.add(this);
  }

  get(key) {
    if (!super.has(key)) return undefined;
    const value = super.get(key);
    super.delete(key);
    super.set(key, value);
    return value;
  }

  set(key, value) {
    super.delete(key);
    super.set(key, value);
    while (this.size > this.max) super.delete(this.keys().next().value);
    return this;
  }
}
