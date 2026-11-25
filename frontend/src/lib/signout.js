import { auth } from './stores/auth.js';
import { player } from './stores/player.js';
import { recentSearches } from './stores/recentSearches.js';

export function clearAccountData() {
  player.clear();
  recentSearches.set([]);
}

export function signOut() {
  clearAccountData();
  auth.logout();
}
