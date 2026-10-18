import { get, writable } from 'svelte/store';
import { DEFAULT_PALETTE, paletteForImage, paletteFrom } from '../color.js';
import { navStore } from './nav.js';

// Цвет текущей страницы (шапка альбома/артиста, вся страница). App.svelte
// красит им фон; смена плавная (transition в CSS).
export const pagePalette = writable(DEFAULT_PALETTE);

const NONE = { ...DEFAULT_PALETTE, base: '#121212', dark: '#121212', header: '#121212', page: '#121212' };
const routeKey = (r) => `${r.view}:${r.id || ''}`;
let current = null; // картинка, палитру которой ждём

/**
 * Окраска фона для страницы, которая её вызвала. Страница берёт её при
 * создании: красить фон может только пока она открыта. Иначе альбом,
 * догрузившийся после возврата на главную, перекрашивал бы главную.
 *   const tint = pageTint();
 *   tint.from(url)   — по картинке (null — нейтральный цвет);
 *   tint.color(rgb)  — заданным цветом ({r, g, b}: у «Любимых» нет обложки);
 *   tint.none()      — без подкраски, ровный фон (дискография).
 */
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
    none() {
      if (!mine()) return;
      current = null;
      pagePalette.set(NONE);
    },
  };
}
