import { writable, get } from 'svelte/store';
import { appleTouch } from '../platform.js';
import { api } from '../api.js';
import { settings, lang } from './settings.js';
import { toast } from './toast.js';
import { translate } from '../i18n.js';
import { loadSavedModes, saveModes, loadSavedQueue, saveQueue, saveQueuePosition, loadSavedVolume, saveVolume } from './player/persist.js';
import {
  followingIndex,
  startQueue,
  shuffleRest,
  restoreOrder,
  beforeAutoplay,
  afterUpNext,
  insertEntries,
  removeEntry,
  swapEntries,
  moveEntry,
  withoutAutoplayed,
} from './player/queue.js';
import { createListenTracker } from './player/listen.js';
import { setPlaybackState, showTrack, updatePositionState, clearTrack } from './player/mediasession.js';
import { createSleepTimer } from './player/sleep.js';
import { createSmoothTime } from './player/smooth.js';
import { select } from './player/select.js';

const QUEUE_SAVE_INTERVAL_MS = 5000;

const PRELOAD_EXTRA_SECONDS = 4;

const GAPLESS_PRELOAD_SECONDS = 10;
const GAPLESS_SCHEDULE_SECONDS = 1.2;
const GAPLESS_LEAD = 0.005;

export const directAudio = appleTouch;

function crossfadeSeconds() {
  const s = get(settings);
  return s.crossfade && !directAudio ? s.crossfadeSeconds : 0;
}

function formatTime(seconds) {
  if (!Number.isFinite(seconds)) return '0:00';
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

function dbToGain(db) {
  return Math.pow(10, (Number(db) || 0) / 20);
}

function createPlayerStore() {
  const saved = loadSavedQueue();
  const state = writable({
    queue: saved?.queue || [],
    index: saved ? saved.index : -1,
    isPlaying: false,
    isLoading: false,
    currentTime: saved?.time || 0,
    duration: saved ? (saved.queue[saved.index].durationMs || 0) / 1000 : 0,
    error: null,
    volume: loadSavedVolume(),
    ...loadSavedModes(),
    sleep: null,
  });

  let unshuffled = saved?.order ? saved.order.map((i) => saved.queue[i]).filter(Boolean) : null;
  const queueOf = (s) => ({ queue: s.queue, index: s.index, unshuffled });

  let restoredPending = !!saved;

  let hooks = { onPlayed: null, onListened: null, autoplay: null, onTrackStart: null, onScrobble: null };
  const listening = createListenTracker(
    () => hooks,
    () => get(state).queue[get(state).index],
  );
  let autoplayPending = null;
  let autoplayFailedAfter = null;

  let lastSaved = { queue: null, unshuffled: null, index: null, at: 0 };
  state.subscribe((s) => {
    const now = Date.now();
    const queueChanged = s.queue !== lastSaved.queue || unshuffled !== lastSaved.unshuffled;
    if (!queueChanged && s.index === lastSaved.index && now - lastSaved.at < QUEUE_SAVE_INTERVAL_MS) return;
    lastSaved = { queue: s.queue, unshuffled, index: s.index, at: now };
    if (queueChanged) saveQueue(s, unshuffled);
    else saveQueuePosition(s);
  });
  if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    window.addEventListener('pagehide', () => {
      saveQueuePosition(get(state));
      listening.flush();
    });
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', () => document.visibilityState === 'hidden' && listening.flush());
    }
  }

  let audioContext = null;
  const elements = [null, null];
  const gains = [null, null];
  const slotGainDb = [0, 0];
  const trackGain = (slot) => (get(settings).normalize ? dbToGain(slotGainDb[slot]) : 1);
  let activeSlot = 0;
  let preloadedForIndex = null;
  let preloadReady = false;
  let fadeStarted = false;
  let retriedTrack = null;
  let loadAuto = false;
  let skipsInRow = 0;
  const MAX_SKIPS = 3;

  async function resolveSource(track) {
    if (track.source) {
      const source = track.source;
      track.source = null;
      return source;
    }
    if (track.id) {
      try {
        const { url, gain_db } = await api.streamUrl(track.id);
        return { url, gainDb: gain_db };
      } catch (e) {
        if (e.status !== 404 || !track.deezerId) throw e;
        track.id = null;
      }
    }
    const r = await api.deezerStream(track.deezerId);
    if (r.track_id) track.id = r.track_id;
    return { url: r.url, gainDb: r.gain_db };
  }

  function inactiveSlot() {
    return activeSlot === 0 ? 1 : 0;
  }

  function ensureContext() {
    if (elements[0]) return;
    if (!directAudio) {
      const Ctx = window.AudioContext || window.webkitAudioContext;
      audioContext = new Ctx();
    }
    for (let i = 0; i < 2; i++) {
      const el = new Audio();
      el.preload = 'auto';
      if (audioContext) {
        const source = audioContext.createMediaElementSource(el);
        const gain = audioContext.createGain();
        gain.gain.value = 0;
        source.connect(gain).connect(audioContext.destination);
        gains[i] = gain;
      } else {
        gains[i] = elementGain(el);
      }
      elements[i] = el;
      attachElementListeners(el, i);
    }
  }

  function elementGain(el) {
    const param = {
      get value() {
        return el.volume;
      },
      set value(v) {
        try {
          el.volume = Math.min(1, Math.max(0, v));
        } catch {
        }
      },
      cancelScheduledValues() {},
      setValueAtTime() {},
      linearRampToValueAtTime() {},
    };
    return { gain: param };
  }

  const ctxTime = () => (audioContext ? audioContext.currentTime : 0);

  function attachElementListeners(el, slot) {
    el.addEventListener('timeupdate', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, currentTime: el.currentTime }));
      sleepTimer.check();
      listening.tick(el);
      if (sleepFade !== null) return;
      maybeStartPreload(el);
      maybeStartCrossfade(el);
      maybeScheduleGapless(el);
    });
    el.addEventListener('playing', () => {
      if (slot !== activeSlot) return;
      retriedTrack = null;
      skipsInRow = 0;
    });
    el.addEventListener('loadedmetadata', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, duration: el.duration }));
    });
    for (const type of ['playing', 'pause', 'seeked', 'durationchange']) {
      el.addEventListener(type, () => {
        if (slot === activeSlot) updatePositionState(el);
      });
    }
    el.addEventListener('play', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, isPlaying: true }));
      setPlaybackState('playing');
    });
    el.addEventListener('pause', () => {
      if (slot !== activeSlot || el.ended) return;
      cancelGaplessTimer();
      listening.paused();
      state.update((s) => ({ ...s, isPlaying: false }));
      setPlaybackState('paused');
    });
    el.addEventListener('waiting', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, isLoading: true }));
    });
    el.addEventListener('canplay', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, isLoading: false }));
    });
    el.addEventListener('ended', () => {
      if (slot !== activeSlot) return;
      handleActiveEnded();
    });
    el.addEventListener('error', () => {
      if (slot !== activeSlot) return;
      handlePlaybackError();
    });
  }

  async function maybeStartPreload(activeEl) {
    const s = get(state);
    const fade = crossfadeSeconds();
    const ahead = Math.max(GAPLESS_PRELOAD_SECONDS, fade + PRELOAD_EXTRA_SECONDS);
    const remaining = activeEl.duration - activeEl.currentTime;
    if (!Number.isFinite(remaining) || remaining > ahead) return;

    if (s.repeat === 'one') return;
    if (s.sleep?.endOfTrack) return;
    const nextIndex = followingIndex(s);
    if (nextIndex === null) {
      if (autoplayOn()) extendWithAutoplay();
      return;
    }
    if (nextIndex === s.index) return;
    if (preloadedForIndex === nextIndex) return;
    if (directAudio) return;

    preloadedForIndex = nextIndex;
    preloadReady = false;
    const nextTrack = s.queue[nextIndex];
    try {
      const { url, gainDb } = await resolveSource(nextTrack);
      if (preloadedForIndex !== nextIndex) return;
      const inactive = inactiveSlot();
      slotGainDb[inactive] = gainDb || 0;
      const el = elements[inactive];
      gains[inactive].gain.value = 0;
      el.src = url;
      el.load();
      preloadReady = true;
    } catch (e) {
      preloadedForIndex = null;
      console.error('crossfade preload failed', e);
    }
  }

  function maybeStartCrossfade(activeEl) {
    const s = get(state);
    if (s.sleep?.endOfTrack) return;
    const nextIndex = followingIndex(s);
    if (nextIndex === null || preloadedForIndex !== nextIndex || !preloadReady) return;
    if (fadeStarted) return;

    const fade = crossfadeSeconds();
    const remaining = activeEl.duration - activeEl.currentTime;
    if (fade === 0 || !Number.isFinite(remaining) || remaining > fade) return;

    fadeStarted = true;
    const dur = Math.max(0.1, Math.min(fade, remaining));
    const now = ctxTime();
    const activeGain = gains[activeSlot];
    const inactive = inactiveSlot();
    const inactiveGain = gains[inactive];
    const targetVolume = s.volume * trackGain(inactive);

    const nextEl = elements[inactive];
    nextEl.currentTime = 0;
    nextEl.play().catch((e) => {
      console.error('crossfade start failed', e);
      nextEl.pause();
      inactiveGain.gain.cancelScheduledValues(ctxTime());
      inactiveGain.gain.value = 0;
      activeGain.gain.cancelScheduledValues(ctxTime());
      activeGain.gain.value = get(state).volume * trackGain(activeSlot);
      preloadReady = false;
      fadeStarted = false;
    });

    activeGain.gain.cancelScheduledValues(now);
    activeGain.gain.setValueAtTime(activeGain.gain.value, now);
    activeGain.gain.linearRampToValueAtTime(0, now + dur);

    inactiveGain.gain.cancelScheduledValues(now);
    inactiveGain.gain.setValueAtTime(inactiveGain.gain.value, now);
    inactiveGain.gain.linearRampToValueAtTime(targetVolume, now + dur);
  }

  let gaplessTimer = null;

  function cancelGaplessTimer() {
    if (gaplessTimer !== null) clearTimeout(gaplessTimer);
    gaplessTimer = null;
  }

  function maybeScheduleGapless(activeEl) {
    if (crossfadeSeconds() !== 0 || fadeStarted || gaplessTimer !== null || activeEl.paused) return;
    const s = get(state);
    if (s.repeat === 'one' || s.sleep?.endOfTrack) return;
    const nextIndex = followingIndex(s);
    if (nextIndex === null || nextIndex === s.index || preloadedForIndex !== nextIndex || !preloadReady) return;
    const remaining = activeEl.duration - activeEl.currentTime;
    if (!Number.isFinite(remaining) || remaining > GAPLESS_SCHEDULE_SECONDS) return;
    const tick = () => {
      gaplessTimer = null;
      if (activeEl !== elements[activeSlot] || activeEl.paused || fadeStarted) return;
      if (preloadedForIndex !== followingIndex(get(state)) || !preloadReady) return;
      const left = activeEl.duration - activeEl.currentTime - GAPLESS_LEAD;
      if (left > 0.004) {
        gaplessTimer = setTimeout(tick, Math.max(0, left * 1000 - (left > 0.05 ? 20 : 0)));
        return;
      }
      startGapless();
    };
    tick();
  }

  function startGapless() {
    fadeStarted = true;
    const inactive = inactiveSlot();
    const nextEl = elements[inactive];
    gains[inactive].gain.cancelScheduledValues(ctxTime());
    gains[inactive].gain.value = get(state).volume * trackGain(inactive);
    nextEl.currentTime = 0;
    nextEl.play().catch((e) => {
      console.error('gapless start failed', e);
      nextEl.pause();
      gains[inactive].gain.value = 0;
      preloadReady = false;
      fadeStarted = false;
    });
  }

  async function handleActiveEnded() {
    listening.newPlay();
    let s = get(state);
    if (s.sleep?.endOfTrack || sleepFade !== null) {
      endSleepFade();
      sleepAfterTrack(s);
      return;
    }
    if (s.repeat === 'one') {
      const el = elements[activeSlot];
      el.currentTime = 0;
      el.play();
      return;
    }
    let nextIndex = followingIndex(s);
    if (nextIndex === null && autoplayOn() && (await extendWithAutoplay())) {
      s = get(state);
      nextIndex = followingIndex(s);
    }
    if (nextIndex === null) {
      state.update((x) => ({ ...x, isPlaying: false, currentTime: 0 }));
      elements[activeSlot].currentTime = 0;
      setPlaybackState('paused');
      return;
    }
    if (!fadeStarted && preloadReady && preloadedForIndex === nextIndex) startGapless();
    if (fadeStarted && preloadedForIndex === nextIndex) {
      completeTransitionTo(nextIndex);
    } else {
      goTo(nextIndex, true);
    }
  }

  function completeTransitionTo(newIndex) {
    const oldSlot = activeSlot;
    const newSlot = inactiveSlot();

    elements[oldSlot].pause();
    gains[oldSlot].gain.value = 0;
    elements[oldSlot].removeAttribute('src');
    elements[oldSlot].load();

    activeSlot = newSlot;
    preloadedForIndex = null;
    preloadReady = false;
    fadeStarted = false;
    listening.newPlay();
    loadAuto = true;

    const track = get(state).queue[newIndex];
    hooks.onTrackStart?.(track);
    state.update((x) => ({
      ...x,
      index: newIndex,
      currentTime: elements[newSlot].currentTime,
      duration: elements[newSlot].duration,
      isPlaying: !elements[newSlot].paused,
      isLoading: false,
    }));
    updateMediaSession(track);
    updatePositionState(elements[newSlot]);
  }

  function cancelPendingCrossfade() {
    if (!elements[0]) return;
    const inactive = inactiveSlot();
    cancelGaplessTimer();
    elements[inactive].pause();
    gains[inactive].gain.cancelScheduledValues(ctxTime());
    gains[inactive].gain.value = 0;
    if (fadeStarted) {
      gains[activeSlot].gain.cancelScheduledValues(ctxTime());
      gains[activeSlot].gain.value = get(state).volume * trackGain(activeSlot);
    }
    preloadedForIndex = null;
    preloadReady = false;
    fadeStarted = false;
  }

  async function handlePlaybackError() {
    const s = get(state);
    const track = s.queue[s.index];
    if (!track) return;
    if (retriedTrack === track) return trackFailed(track);
    retriedTrack = track;
    await loadTrack(track, elements[activeSlot].currentTime, loadAuto);
  }

  async function trackFailed(track, err = null) {
    const notFound = err?.data?.reason === 'not_found';
    const text = translate(get(lang), notFound ? 'player.notFound' : 'player.failed', { title: track.title });
    state.update((x) => ({ ...x, error: text, isPlaying: false, isLoading: false }));
    toast(text, 'error', notFound ? 6000 : undefined);
    if (!loadAuto || skipsInRow >= MAX_SKIPS) return;
    const s = get(state);
    const nextIndex = followingIndex(s);
    if (nextIndex === null || nextIndex === s.index) return;
    skipsInRow++;
    await goTo(nextIndex, true);
  }

  const updateMediaSession = (track) => showTrack(track, { play: resume, pause, prev, next, seek });

  let loadSeq = 0;

  async function loadTrack(track, resumeAt = 0, auto = false) {
    const seq = ++loadSeq;
    loadAuto = auto;
    if (!auto) skipsInRow = 0;
    ensureContext();
    endSleepFade();
    restoredPending = false;
    if (resumeAt === 0) listening.newPlay();
    if (audioContext?.state === 'suspended') {
      try {
        await audioContext.resume();
      } catch {
      }
      if (seq !== loadSeq) return;
    }
    cancelPendingCrossfade();

    state.update((s) => ({ ...s, isLoading: true, error: null }));
    hooks.onTrackStart?.(track);
    let source;
    try {
      source = await resolveSource(track);
    } catch (e) {
      if (seq !== loadSeq) return;
      console.error('stream link failed', e);
      elements[activeSlot].pause();
      return trackFailed(track, e);
    }
    if (seq !== loadSeq) return;
    const { url, gainDb } = source;
    const el = elements[activeSlot];
    el.src = url;
    slotGainDb[activeSlot] = gainDb || 0;
    gains[activeSlot].gain.value = get(state).volume * trackGain(activeSlot);
    updateMediaSession(track);
    if (resumeAt > 0) {
      const onLoaded = () => {
        el.currentTime = resumeAt;
        el.removeEventListener('loadedmetadata', onLoaded);
      };
      el.addEventListener('loadedmetadata', onLoaded);
    }
    try {
      await el.play();
      skipsInRow = 0;
    } catch (e) {
      if (seq !== loadSeq || e?.name === 'AbortError') return;
      state.update((x) => ({ ...x, isLoading: false }));
    }
  }

  async function playQueue(tracks, startIndex = 0, { chosen = true } = {}) {
    const q = startQueue(tracks, startIndex, get(state).shuffle);
    unshuffled = q.unshuffled;
    state.update((s) => ({ ...s, queue: q.queue, index: q.index }));
    await loadTrack(q.queue[q.index], 0, !chosen);
  }

  function toggleShuffle() {
    cancelPendingCrossfade();
    const s = get(state);
    const q = s.shuffle ? restoreOrder(queueOf(s)) : shuffleRest(queueOf(s));
    unshuffled = q.unshuffled;
    state.update((x) => ({ ...x, shuffle: !s.shuffle, queue: q.queue, index: q.index }));
    saveModes(get(state));
  }

  function cycleRepeat() {
    cancelPendingCrossfade();
    state.update((x) => ({ ...x, repeat: x.repeat === 'off' ? 'all' : x.repeat === 'all' ? 'one' : 'off' }));
    saveModes(get(state));
  }

  function playTrack(track) {
    return playQueue([track], 0);
  }

  function addToQueue(track) {
    return appendToQueue([track]);
  }

  function appendToQueue(tracks) {
    if (tracks.length === 0) return Promise.resolve();
    if (get(state).queue.length === 0) return playQueue(tracks, 0);
    return insert(tracks, beforeAutoplay);
  }

  function playNext(track) {
    if (get(state).queue.length === 0) return playQueue([track], 0);
    return insert([{ ...track, upNext: true }], afterUpNext);
  }

  function insert(entries, place) {
    const q = insertEntries(queueOf(get(state)), entries, place);
    if (preloadedForIndex !== null && q.at <= preloadedForIndex) cancelPendingCrossfade();
    unshuffled = q.unshuffled;
    state.update((x) => ({ ...x, queue: q.queue }));
    return Promise.resolve();
  }

  async function removeFromQueue(idx) {
    const s = get(state);
    if (idx < 0 || idx >= s.queue.length) return;
    cancelPendingCrossfade();

    const q = removeEntry(queueOf(s), idx);
    unshuffled = q.unshuffled;
    const wasCurrent = idx === s.index;
    if (wasCurrent && q.queue.length === 0) pause();
    state.update((x) => ({ ...x, queue: q.queue, index: q.index }));
    if (wasCurrent && q.queue.length > 0) await loadTrack(q.queue[q.index], 0, true);
  }

  function moveInQueue(fromIdx, direction) {
    const s = get(state);
    const toIdx = fromIdx + direction;
    if (toIdx < 0 || toIdx >= s.queue.length) return;
    cancelPendingCrossfade();
    const q = swapEntries(queueOf(s), fromIdx, toIdx);
    state.update((x) => ({ ...x, queue: q.queue, index: q.index }));
  }

  function moveInQueueTo(fromIdx, toIdx) {
    const s = get(state);
    const n = s.queue.length;
    if (fromIdx === toIdx || fromIdx < 0 || toIdx < 0 || fromIdx >= n || toIdx >= n) return;
    cancelPendingCrossfade();
    const q = moveEntry(queueOf(s), fromIdx, toIdx);
    state.update((x) => ({ ...x, queue: q.queue, index: q.index }));
  }

  async function jumpTo(idx) {
    const s = get(state);
    if (idx < 0 || idx >= s.queue.length || idx === s.index) return;
    state.update((x) => ({ ...x, index: idx }));
    await loadTrack(s.queue[idx]);
  }

  function pause() {
    if (elements[activeSlot]) elements[activeSlot].pause();
    endSleepFade();
  }

  function clear() {
    loadSeq++;
    cancelPendingCrossfade();
    for (const el of elements) el?.pause();
    endSleepFade();
    setSleepTimer(null);
    unshuffled = null;
    restoredPending = false;
    state.update((s) => ({ ...s, queue: [], index: -1, isPlaying: false, isLoading: false, currentTime: 0, duration: 0, error: null }));
    clearTrack();
  }

  function playRestored() {
    const s = get(state);
    const track = s.queue[s.index];
    if (!track) return Promise.resolve();
    return loadTrack(track, s.currentTime).catch((e) => {
      console.error('restore failed', e);
      state.update((x) => ({ ...x, isLoading: false }));
    });
  }

  function resume() {
    if (restoredPending) return playRestored();
    endSleepFade();
    if (elements[activeSlot]) elements[activeSlot].play();
  }

  function toggle() {
    if (restoredPending) return playRestored();
    const el = elements[activeSlot];
    if (!el) return;
    if (el.paused) {
      endSleepFade();
      el.play();
    } else {
      pause();
    }
  }

  async function goTo(index, auto = false) {
    const s = get(state);
    state.update((x) => ({ ...x, index }));
    await loadTrack(s.queue[index], 0, auto);
  }

  async function next() {
    let i = followingIndex(get(state));
    if (i === null && autoplayOn() && (await extendWithAutoplay())) i = followingIndex(get(state));
    if (i !== null) await goTo(i, true);
  }

  async function prev({ always = false } = {}) {
    const s = get(state);
    const el = restoredPending ? null : elements[activeSlot];
    if (el && el.currentTime > 3 && !(always && s.index > 0)) {
      el.currentTime = 0;
      return;
    }
    if (s.index > 0) await goTo(s.index - 1);
    else if (s.repeat !== 'off' && s.queue.length > 1) await goTo(s.queue.length - 1);
    else if (el) el.currentTime = 0;
  }

  function canGoNext(s, autoplay = get(settings).autoplay) {
    return followingIndex(s) !== null || (autoplay && s.repeat === 'off' && s.queue.length > 0 && !!hooks.autoplay);
  }

  function setHooks(h) {
    hooks = { ...hooks, ...h };
  }

  function patchEntry(entry, fields) {
    Object.assign(entry, fields);
    const s = get(state);
    if (s.queue[s.index] === entry) updateMediaSession(entry);
    state.update((x) => ({ ...x, queue: [...x.queue] }));
  }

  function autoplayOn() {
    return get(settings).autoplay && get(state).repeat === 'off';
  }

  function extendWithAutoplay() {
    if (!hooks.autoplay) return Promise.resolve(false);
    if (autoplayPending) return autoplayPending;
    const s = get(state);
    const last = s.queue[s.queue.length - 1];
    if (!last || autoplayFailedAfter === last) return Promise.resolve(false);
    autoplayPending = Promise.resolve()
      .then(() => hooks.autoplay(last, s.queue))
      .then((entries) => {
        const fresh = (entries || []).map((e) => ({ ...e, autoplay: true }));
        if (fresh.length === 0 || get(state).queue[get(state).queue.length - 1] !== last) {
          if (fresh.length === 0) autoplayFailedAfter = last;
          return false;
        }
        if (unshuffled) unshuffled = [...unshuffled, ...fresh];
        state.update((x) => ({ ...x, queue: [...x.queue, ...fresh] }));
        return true;
      })
      .catch((e) => {
        console.error('autoplay failed', e);
        autoplayFailedAfter = last;
        return false;
      })
      .finally(() => {
        autoplayPending = null;
      });
    return autoplayPending;
  }

  function dropAutoplayed() {
    const s = get(state);
    if (!s.queue.some((e, i) => e.autoplay && i > s.index)) return;
    cancelPendingCrossfade();
    const q = withoutAutoplayed(queueOf(s));
    unshuffled = q.unshuffled;
    state.update((x) => ({ ...x, queue: q.queue }));
  }

  const SLEEP_FADE_SECONDS = 3;
  const sleepTimer = createSleepTimer(state, fadeOutAndPause);

  function setSleepTimer(minutes) {
    if (minutes === 'track') cancelPendingCrossfade();
    sleepTimer.set(minutes);
  }

  let sleepFade = null;

  function fadeOutAndPause() {
    const el = elements[activeSlot];
    if (!el || el.paused) return;
    cancelPendingCrossfade();
    if (directAudio) {
      el.pause();
      return;
    }
    const gain = gains[activeSlot];
    const now = ctxTime();
    gain.gain.cancelScheduledValues(now);
    gain.gain.setValueAtTime(gain.gain.value, now);
    gain.gain.linearRampToValueAtTime(0, now + SLEEP_FADE_SECONDS);
    sleepFade = setTimeout(pause, SLEEP_FADE_SECONDS * 1000);
  }

  function endSleepFade() {
    if (sleepFade === null) return;
    clearTimeout(sleepFade);
    sleepFade = null;
    gains[activeSlot].gain.cancelScheduledValues(ctxTime());
    gains[activeSlot].gain.value = get(state).volume * trackGain(activeSlot);
  }

  function sleepAfterTrack(s) {
    cancelPendingCrossfade();
    setPlaybackState('paused');
    const nextIndex = followingIndex(s);
    if (nextIndex === null || nextIndex === s.index) {
      state.update((x) => ({ ...x, sleep: null, isPlaying: false, currentTime: 0 }));
      elements[activeSlot].currentTime = 0;
      return;
    }
    const track = s.queue[nextIndex];
    restoredPending = true;
    state.update((x) => ({ ...x, sleep: null, index: nextIndex, isPlaying: false, currentTime: 0, duration: (track.durationMs || 0) / 1000 }));
    updateMediaSession(track);
  }

  function seek(time) {
    if (restoredPending) {
      state.update((s) => ({ ...s, currentTime: time }));
      return;
    }
    const el = elements[activeSlot];
    if (!el) return;
    cancelGaplessTimer();
    el.currentTime = time;
    state.update((s) => ({ ...s, currentTime: time }));
  }

  function position() {
    const el = elements[activeSlot];
    return el && !restoredPending ? el.currentTime : get(state).currentTime;
  }

  const smoothTime = createSmoothTime(state, position);

  function setVolume(v) {
    const clamped = Math.min(1, Math.max(0, v));
    if (gains[activeSlot]) gains[activeSlot].gain.value = clamped * trackGain(activeSlot);
    saveVolume(clamped);
    state.update((s) => ({ ...s, volume: clamped }));
  }

  let autoplayWas = get(settings).autoplay;
  settings.subscribe((st) => {
    if (gains[activeSlot] && !fadeStarted) gains[activeSlot].gain.value = get(state).volume * trackGain(activeSlot);
    if (autoplayWas && !st.autoplay) dropAutoplayed();
    autoplayWas = st.autoplay;
  });

  return {
    subscribe: state.subscribe,
    playTrack,
    playQueue,
    addToQueue,
    appendToQueue,
    playNext,
    removeFromQueue,
    moveInQueue,
    moveInQueueTo,
    jumpTo,
    pause,
    resume,
    toggle,
    next,
    prev,
    seek,
    position,
    smoothTime,
    setVolume,
    toggleShuffle,
    cycleRepeat,
    canGoNext,
    setHooks,
    patchEntry,
    setSleepTimer,
    clear,
  };
}

export const player = createPlayerStore();
export { formatTime, select };

export const nowPlaying = select(player, (s) => [s.queue, s.index], (s) => s.queue[s.index] ?? null);
export const isPlaying = select(player, (s) => [s.isPlaying]);
export const shuffleOn = select(player, (s) => [s.shuffle]);
export const queueState = select(
  player,
  (s) => [s.queue, s.index, s.shuffle, s.repeat],
  (s) => ({ queue: s.queue, index: s.index, shuffle: s.shuffle, repeat: s.repeat }),
);
