const pageCaches = new Set();

export function clearPageCaches() {
  for (const c of pageCaches) c.clear();
}

export class LRU extends Map {
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
