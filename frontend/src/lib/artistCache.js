// Ответы /api/discover/artists/<id> между заходами — общие для страницы
// артиста и его дискографии: «назад» и переход «Показать все» показывают
// уже загруженное сразу, свежие данные подгружаются следом.
import { LRU } from './lru.js';

export const artistPages = new LRU(20); // deezer id артиста -> { artist, albums, top }
