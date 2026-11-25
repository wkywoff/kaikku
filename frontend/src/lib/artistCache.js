import { LRU } from './lru.js';

export const artistPages = new LRU(20);
export const artistInfos = new LRU(50);
export const artistBios = new LRU(30);
