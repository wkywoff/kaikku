// Таймер сна: через N минут — пауза; «до конца трека» — после текущего
// трека дальше не играем (это делает сам плеер, здесь только пометка).
// Таймер живёт до перезагрузки страницы. В состоянии плеера:
//
//   sleep: null | { at } — пауза в этот момент (мс) | { endOfTrack: true }

import { get } from 'svelte/store';

/** state — стор плеера (writable); onTime — время вышло, пора на паузу. */
export function createSleepTimer(state, onTime) {
  let handle = null;

  function stop() {
    clearTimeout(handle);
    handle = null;
  }

  /** minutes: число минут, 'track' — до конца трека, null — выключить. */
  function set(minutes) {
    stop();
    let sleep = null;
    if (minutes === 'track') {
      sleep = { endOfTrack: true };
    } else if (minutes > 0) {
      sleep = { at: Date.now() + minutes * 60000 };
      handle = setTimeout(check, minutes * 60000);
    }
    state.update((s) => ({ ...s, sleep }));
  }

  // Плеер зовёт и сам, на каждый timeupdate: в фоне таймер браузера может
  // сработать не вовремя.
  function check() {
    const at = get(state).sleep?.at;
    if (!at || Date.now() < at) return;
    stop();
    state.update((s) => ({ ...s, sleep: null }));
    onTime();
  }

  return { set, check };
}
