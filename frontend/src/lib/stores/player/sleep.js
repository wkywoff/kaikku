import { get } from 'svelte/store';

export function createSleepTimer(state, onTime) {
  let handle = null;

  function stop() {
    clearTimeout(handle);
    handle = null;
  }

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

  function check() {
    const at = get(state).sleep?.at;
    if (!at || Date.now() < at) return;
    stop();
    state.update((s) => ({ ...s, sleep: null }));
    onTime();
  }

  return { set, check };
}
