export function placeholderHue(id) {
  let h = 0x811c9dc5;
  for (const b of new TextEncoder().encode(String(id))) {
    h ^= b;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  h ^= h >>> 16;
  h = Math.imul(h, 0x85ebca6b) >>> 0;
  h ^= h >>> 13;
  h = Math.imul(h, 0xc2b2ae35) >>> 0;
  h ^= h >>> 16;
  return (h >>> 0) % 360;
}

export function placeholderBackground(id) {
  const hue = placeholderHue(id);
  return [
    `radial-gradient(circle, hsl(${hue} 32% 13%) 0 1.2%, hsl(${hue} 50% 70%) 1.3% 10.5%, rgba(0, 0, 0, 0.6) 10.6% 31%, transparent 31.1%)`,
    `linear-gradient(135deg, hsl(${hue} 55% 46%), hsl(${(hue + 40) % 360} 60% 20%))`,
  ].join(', ');
}
