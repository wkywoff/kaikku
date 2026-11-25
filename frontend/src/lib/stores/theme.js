import { get, writable } from 'svelte/store';
import { DEFAULT_PALETTE, paletteForImage, paletteFrom } from '../color.js';
import { navStore } from './nav.js';

export const pagePalette = writable(DEFAULT_PALETTE);

const NONE = { ...DEFAULT_PALETTE, base: '#121212', dark: '#121212', header: '#121212', page: '#121212' };
const routeKey = (r) => `${r.view}:${r.id || ''}`;
let current = null;

export function pageTint() {
  const owner = routeKey(get(navStore));
  const mine = () => owner === routeKey(get(navStore));
  return {
    from(imageUrl) {
      if (!mine()) return;
      current = imageUrl;
      if (!imageUrl) {
        pagePalette.set(DEFAULT_PALETTE);
        return;
      }
      paletteForImage(imageUrl).then((p) => {
        if (current === imageUrl && mine()) pagePalette.set(p);
      });
    },
    color(rgb) {
      if (!mine()) return;
      current = null;
      pagePalette.set(paletteFrom(rgb));
    },
    none(accentOf = null) {
      if (!mine()) return;
      current = null;
      const { accent, accentText } = accentOf ? paletteFrom(accentOf) : NONE;
      pagePalette.set({ ...NONE, accent, accentText });
    },
  };
}
