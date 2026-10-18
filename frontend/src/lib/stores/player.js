import { writable, readable, get } from 'svelte/store';
import { appleTouch } from '../platform.js';
import { api } from '../api.js';
import { settings, lang } from './settings.js';
import { artworkFor } from '../imageproxy.js';
import { toast } from './toast.js';
import { translate } from '../i18n.js';

const VOLUME_STORAGE_KEY = 'kaikku_volume';
const MODES_STORAGE_KEY = 'kaikku_modes';
const QUEUE_STORAGE_KEY = 'kaikku_queue';

// Прослушивание записывается в историю, когда трек проиграл столько секунд
// (короткий — половину), как у Spotify.
const PLAY_REPORT_SECONDS = 30;
const SCROBBLE_MIN_SECONDS = 30;
const SCROBBLE_MAX_SECONDS = 240;
// Позиция в сохранённой очереди обновляется не чаще этого.
const QUEUE_SAVE_INTERVAL_MS = 5000;

// Кроссфейд: за max(GAPLESS_PRELOAD_SECONDS, переход + PRELOAD_EXTRA_SECONDS) до конца
// трека второй <audio>-элемент получает ссылку на следующий трек и начинает
// буферизовать его (load(), не play() — иначе к началу фейда следующий трек
// уже проиграл бы несколько секунд впустую). За «длительность перехода» до
// конца он стартует с нуля, и громкости двух слотов плавно меняются
// местами. Длительность и сам переход — в настройках (stores/settings.js).
// Кроссфейд только на естественном окончании трека — ручное переключение
// всегда мгновенное.
const PRELOAD_EXTRA_SECONDS = 4;

// Без кроссфейда — бесшовный (gapless) переход: следующий трек так же
// заранее грузится во второй слот, а стартует ровно тогда, когда кончается
// текущий. Таймер ставится за ~1 с до конца и перепроверяется по
// currentTime прямо перед стартом. Замер в Chrome: подготовленный элемент
// начинает идти сразу после play(), так что старт — в самый конец, с
// запасом GAPLESS_LEAD на то, что setTimeout срабатывает на пару мс позже.
const GAPLESS_PRELOAD_SECONDS = 10;
const GAPLESS_SCHEDULE_SECONDS = 1.2;
const GAPLESS_LEAD = 0.005;

// iPhone и iPad: звук идёт прямо из <audio>, без Web Audio. В свёрнутом
// приложении iOS замораживает Web Audio, а <audio> продолжает идти —
// время бежит, звука нет, а после возврата звук залипал на одной секунде.
// Без Web Audio нет и того, что на нём держится: кроссфейда, ползунка
// громкости (на iPhone она — кнопками телефона) и выравнивания громкости;
// треки идут друг за другом в одном элементе.
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

// Поправка громкости трека (gain_db из stream-url) — выравнивание до
// -14 LUFS делается здесь, а не перекодированием файла на сервере, и только
// если оно включено в настройках (по умолчанию нет — громкость оригинала).
function dbToGain(db) {
  return Math.pow(10, (Number(db) || 0) / 20);
}

function loadSavedModes() {
  try {
    const m = JSON.parse(localStorage.getItem(MODES_STORAGE_KEY) || '{}');
    return { shuffle: !!m.shuffle, repeat: ['off', 'all', 'one'].includes(m.repeat) ? m.repeat : 'off' };
  } catch {
    return { shuffle: false, repeat: 'off' };
  }
}

/** Fisher–Yates; random — для тестов. */
export function shuffled(items, random = Math.random) {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

// Очередь переживает перезагрузку страницы: треки, какой играл и с какого
// места (как Spotify, трек ждёт на паузе). source — одноразовая ссылка, её
// не сохраняем.
function loadSavedQueue() {
  try {
    const q = JSON.parse(localStorage.getItem(QUEUE_STORAGE_KEY) || 'null');
    if (!q || !Array.isArray(q.queue) || q.queue.length === 0) return null;
    const index = Number.isInteger(q.index) && q.index >= 0 && q.index < q.queue.length ? q.index : 0;
    const order = Array.isArray(q.order) && q.order.length === q.queue.length ? q.order : null;
    return { queue: q.queue, index, time: Math.max(0, Number(q.time) || 0), order };
  } catch {
    return null;
  }
}

function saveQueue(s, unshuffled) {
  try {
    if (s.queue.length === 0) {
      localStorage.removeItem(QUEUE_STORAGE_KEY);
      return;
    }
    const queue = s.queue.map(({ source, ...rest }) => rest);
    const order = unshuffled ? unshuffled.map((e) => s.queue.indexOf(e)) : null;
    localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify({ queue, index: s.index, time: s.currentTime, order }));
  } catch {
    /* переполнено или приватный режим — после перезагрузки очередь просто пустая */
  }
}

function loadSavedVolume() {
  const saved = localStorage.getItem(VOLUME_STORAGE_KEY);
  if (saved === null) return 1;
  const n = Number(saved);
  return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 1;
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
    ...loadSavedModes(), // shuffle: bool, repeat: 'off' | 'all' | 'one'
    // Таймер сна: null | { at } — пауза в этот момент (мс) | { endOfTrack: true }.
    sleep: null,
  });

  // Порядок очереди без перемешивания: к нему возвращаемся, когда shuffle
  // выключают. Элементы — те же объекты, что в queue.
  let unshuffled = saved?.order ? saved.order.map((i) => saved.queue[i]).filter(Boolean) : null;

  // Очередь восстановлена из localStorage, но трек ещё не загружен: первый
  // play() загрузит его с сохранённого места.
  let restoredPending = !!saved;

  // Хуки из lib/library.js (тот импортирует этот модуль, а не наоборот):
  //   onPlayed(entry)          — трек дослушан до порога, записать в историю;
  //   autoplay(last, queue)    — чем продолжить, когда очередь кончилась
  //                              (Promise со списком элементов очереди);
  //   onTrackStart(entry)      — трек стал текущим (дозапросить фиты);
  //   onScrobble(entry, startedAt) — дослушан до порога Last.fm (половина
  //                              или 4 минуты); startedAt — unix-секунды начала.
  let hooks = { onPlayed: null, autoplay: null, onTrackStart: null, onScrobble: null };
  let playReported = false; // за текущее проигрывание трека уже записали
  let scrobbled = false; // и отправили в Last.fm
  let autoplayPending = null;
  let autoplayFailedAfter = null; // последний элемент очереди, после которого подобрать не вышло

  let lastSaved = { queue: null, index: null, at: 0 };
  state.subscribe((s) => {
    const now = Date.now();
    if (s.queue === lastSaved.queue && s.index === lastSaved.index && now - lastSaved.at < QUEUE_SAVE_INTERVAL_MS) return;
    lastSaved = { queue: s.queue, index: s.index, at: now };
    saveQueue(s, unshuffled);
  });
  if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    window.addEventListener('pagehide', () => saveQueue(get(state), unshuffled));
  }

  // Web Audio граф создаётся лениво (нужен user gesture) — два <audio> +
  // GainNode на каждый, оба в общий destination. "Активный" слот — тот, что
  // сейчас слышен и отражается в видимом состоянии (currentTime/duration/
  // isPlaying); второй тихо (gain=0) готовит следующий трек к кроссфейду.
  let audioContext = null;
  const elements = [null, null];
  const gains = [null, null];
  const slotGainDb = [0, 0]; // gain_db трека в каждом слоте; итог = volume * trackGain(slot)
  const trackGain = (slot) => (get(settings).normalize ? dbToGain(slotGainDb[slot]) : 1);
  let activeSlot = 0;
  let preloadedForIndex = null; // index трека, для которого начата предзагрузка
  let preloadReady = false; // ссылка получена и выставлена неактивному слоту
  let fadeStarted = false;
  let retriedTrack = null; // трек (элемент очереди), для которого уже была попытка восстановиться после ошибки
  // Трек включился сам (переход по очереди, «Далее», Play альбома), а не
  // кликом по нему: не сыграв, такой пропускается — но подряд не больше
  // MAX_SKIPS, вдруг сломалось всё (YouTube, сеть), а не один трек.
  let loadAuto = false;
  let skipsInRow = 0;
  const MAX_SKIPS = 3;

  // Откуда играть трек. Обычно — подписанная ссылка на файл медиатеки по
  // track.id. Трек, которого ещё нет в медиатеке (id: null, есть deezerId),
  // играет потоком, пока скачивается; source — ссылка, уже полученная при
  // клике (lib/library.js → playRows), она годится один раз. Дальше —
  // снова через сервер: когда трек скачается, тот вернёт уже ссылку на
  // медиатеку и его id.
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
        // Файл удалили на сервере как неслушаемый (AUDIO_KEEP_DAYS) — трек
        // снова идёт через Deezer: поток сразу, а сервер скачает его заново.
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

  // Какой трек следующий с учётом повтора: repeat 'all' зацикливает
  // очередь. Повтор одного трека здесь не учитывается — его обрабатывает
  // handleActiveEnded (перемотка в начало, без кроссфейда в самого себя).
  function followingIndex(s) {
    if (s.index + 1 < s.queue.length) return s.index + 1;
    if (s.repeat !== 'off' && s.queue.length > 0) return 0;
    return null;
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

  // Без Web Audio (directAudio) «усиление» — громкость самого элемента
  // (на iPhone её не поменять — там она всегда полная), и без плавных
  // переходов: кроссфейда в этом режиме нет, затухания тоже.
  function elementGain(el) {
    const param = {
      get value() {
        return el.volume;
      },
      set value(v) {
        try {
          el.volume = Math.min(1, Math.max(0, v));
        } catch {
          /* только для чтения */
        }
      },
      cancelScheduledValues() {},
      setValueAtTime() {},
      linearRampToValueAtTime() {},
    };
    return { gain: param };
  }

  // Время для расписания громкости; без Web Audio расписания нет.
  const ctxTime = () => (audioContext ? audioContext.currentTime : 0);

  function attachElementListeners(el, slot) {
    el.addEventListener('timeupdate', () => {
      if (slot !== activeSlot) return; // тихий (неактивный) элемент не должен трогать видимое состояние
      state.update((s) => ({ ...s, currentTime: el.currentTime }));
      maybeSleep(); // на случай, если таймер в фоне сработал не вовремя
      updatePositionState(el);
      maybeReportPlay(el);
      maybeStartPreload(el);
      maybeStartCrossfade(el);
      maybeScheduleGapless(el);
    });
    el.addEventListener('playing', () => {
      if (slot !== activeSlot) return;
      retriedTrack = null; // трек реально заиграл — следующая ошибка снова заслуживает повтора
      skipsInRow = 0;
    });
    el.addEventListener('loadedmetadata', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, duration: el.duration }));
    });
    el.addEventListener('play', () => {
      if (slot !== activeSlot) return;
      state.update((s) => ({ ...s, isPlaying: true }));
      setPlaybackState('playing');
    });
    el.addEventListener('pause', () => {
      // На естественном конце трека браузер шлёт 'pause' перед 'ended' — это
      // не пауза пользователя: следующий трек уже играет во втором слоте.
      if (slot !== activeSlot || el.ended) return;
      cancelGaplessTimer(); // пауза/перемотка у конца — перепланируем по следующему timeupdate
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
    // Не позже чем за 10 с (нескачанному треку нужно время найтись и
    // начать грузиться), а при длинном переходе — за переход + запас.
    const ahead = Math.max(GAPLESS_PRELOAD_SECONDS, fade + PRELOAD_EXTRA_SECONDS);
    const remaining = activeEl.duration - activeEl.currentTime;
    if (!Number.isFinite(remaining) || remaining > ahead) return;

    if (s.repeat === 'one') return; // тот же трек начнётся заново, кроссфейд не нужен
    if (s.sleep?.endOfTrack) return; // после этого трека — тишина
    const nextIndex = followingIndex(s);
    if (nextIndex === null) {
      // Очередь кончается — подбираем продолжение заранее, чтобы переход
      // был таким же плавным; следующий timeupdate его и подготовит.
      if (autoplayOn()) extendWithAutoplay();
      return;
    }
    if (nextIndex === s.index) return;
    if (preloadedForIndex === nextIndex) return; // уже начали для этого перехода
    if (directAudio) return; // один элемент: следующий трек грузится по окончании (handleActiveEnded)

    preloadedForIndex = nextIndex;
    preloadReady = false;
    const nextTrack = s.queue[nextIndex];
    try {
      const { url, gainDb } = await resolveSource(nextTrack);
      if (preloadedForIndex !== nextIndex) return; // очередь успела измениться
      const inactive = inactiveSlot();
      slotGainDb[inactive] = gainDb || 0;
      const el = elements[inactive];
      gains[inactive].gain.value = 0;
      el.src = url;
      el.load(); // только буферизация; play() — в момент начала фейда / конца трека
      preloadReady = true;
    } catch (e) {
      // Не смогли подготовить заранее — не критично, просто не будет
      // кроссфейда для этого перехода; handleActiveEnded() ниже подстрахует
      // обычным next().
      preloadedForIndex = null;
      console.error('crossfade preload failed', e);
    }
  }

  function maybeStartCrossfade(activeEl) {
    const s = get(state);
    if (s.sleep?.endOfTrack) return;
    const nextIndex = followingIndex(s);
    if (nextIndex === null || preloadedForIndex !== nextIndex || !preloadReady) return; // нечего фейдить (не готово либо очередь успела измениться)
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
      // Не стартовал (например, autoplay-политика) — отменяем фейд, текущий
      // трек доигрывает на полной громкости, а ended() перейдёт обычным next().
      // preloadedForIndex не сбрасываем — иначе следующий timeupdate начал
      // бы предзагрузку заново.
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

  // Бесшовный переход (кроссфейд выключен): за ~1 с до конца ставим таймер
  // на момент «конец минус задержка запуска».
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
      // setTimeout неточен на несколько мс: подходим к моменту в два захода.
      if (left > 0.004) {
        gaplessTimer = setTimeout(tick, Math.max(0, left * 1000 - (left > 0.05 ? 20 : 0)));
        return;
      }
      startGapless();
    };
    tick();
  }

  function startGapless() {
    fadeStarted = true; // дальше — как после кроссфейда: ended() → completeTransitionTo
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
      fadeStarted = false; // ended() перейдёт обычным next()
    });
  }

  async function handleActiveEnded() {
    let s = get(state);
    if (s.sleep?.endOfTrack) {
      sleepAfterTrack(s);
      return;
    }
    if (s.repeat === 'one') {
      const el = elements[activeSlot];
      el.currentTime = 0;
      playReported = scrobbled = false; // повтор — новое прослушивание
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
      return;
    }
    // Следующий трек уже загружен, но таймер бесшовного старта не успел —
    // стартуем его сейчас, это всё равно быстрее новой загрузки.
    if (!fadeStarted && preloadReady && preloadedForIndex === nextIndex) startGapless();
    if (fadeStarted && preloadedForIndex === nextIndex) {
      // Следующий трек уже играет во втором слоте — просто делаем его активным.
      completeTransitionTo(nextIndex);
    } else {
      // Фейд не успел начаться (короткий трек/медленная сеть) — обычный путь.
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
    playReported = scrobbled = false;
    loadAuto = true; // включился сам: не сыграет — пропустим (trackFailed)

    const track = get(state).queue[newIndex];
    hooks.onTrackStart?.(track);
    state.update((x) => ({
      ...x,
      index: newIndex,
      currentTime: elements[newSlot].currentTime,
      duration: elements[newSlot].duration,
      isPlaying: !elements[newSlot].paused, // события 'play' нового слота пришли, пока он был неактивным
      isLoading: false,
    }));
    updateMediaSession(track);
  }

  // cancelPendingCrossfade — глушит и сбрасывает неактивный слот. Нужно на
  // любом ручном изменении очереди/навигации, чтобы не оставлять тихо
  // играющий "осиротевший" preload, если он больше не актуален. Если фейд
  // уже шёл, текущему треку возвращается громкость — иначе он доиграл бы
  // беззвучно.
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

  // Подписанная ссылка живёт ограниченный срок (MEDIA_TTL_SECONDS) — если трек
  // был на паузе дольше этого или перемотка пришлась на момент истечения,
  // одна попытка молча перевыпустить ссылку и продолжить с того же места,
  // прежде чем показывать ошибку пользователю. Флаг сбрасывается только
  // когда трек реально заиграл ('playing'), иначе битый файл давал бы
  // бесконечный цикл ошибка → новая ссылка → ошибка.
  async function handlePlaybackError() {
    const s = get(state);
    const track = s.queue[s.index];
    if (!track) return;
    if (retriedTrack === track) return trackFailed(track);
    retriedTrack = track;
    await loadTrack(track, elements[activeSlot].currentTime, loadAuto);
  }

  // Трек так и не сыграл (сервер не дал ссылку, файл не открылся и после
  // повтора): сообщение, а если он включился сам — следующий трек.
  // err — почему: на YouTube не нашлось (сервер, reason not_found) —
  // так и пишем, и тост дольше: это не сбой, повтор не поможет.
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

  // Кнопка на экране блокировки (iOS, Android) — по тому, что на самом деле:
  // без этого после паузы в приложении она так и показывала «пауза».
  function setPlaybackState(value) {
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = value;
  }

  function updateMediaSession(track) {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: track.title,
      artist: track.artist || '',
      album: track.album || '',
      artwork: artworkFor(track.coverUrl),
    });
    navigator.mediaSession.setActionHandler('play', () => resume());
    navigator.mediaSession.setActionHandler('pause', () => pause());
    navigator.mediaSession.setActionHandler('previoustrack', () => prev());
    navigator.mediaSession.setActionHandler('nexttrack', () => next());
    navigator.mediaSession.setActionHandler('seekto', (d) => seek(d.seekTime));
  }

  function updatePositionState(el) {
    if (!('mediaSession' in navigator) || !navigator.mediaSession.setPositionState) return;
    if (!Number.isFinite(el.duration) || el.duration <= 0) return;
    try {
      navigator.mediaSession.setPositionState({
        duration: el.duration,
        position: Math.min(el.currentTime, el.duration),
        playbackRate: 1,
      });
    } catch {
      /* некоторые браузеры кидают на граничных значениях — не критично */
    }
  }

  // Номер последней загрузки: ссылку на нескачанный трек сервер ищет
  // секунды, и если за это время включили другой трек, запоздавший ответ не
  // должен его перебить.
  let loadSeq = 0;

  // loadTrack — прямая, немедленная загрузка в АКТИВНЫЙ слот. Используется
  // для всей ручной навигации (playQueue/jumpTo/next/prev и т.д.) — всегда
  // мгновенно, без кроссфейда; фейд случается только через
  // completeTransitionTo() на естественном окончании трека.
  // auto — трек включился сам, а не кликом по нему (см. trackFailed).
  async function loadTrack(track, resumeAt = 0, auto = false) {
    const seq = ++loadSeq;
    loadAuto = auto;
    if (!auto) skipsInRow = 0;
    ensureContext();
    restoredPending = false;
    // Перезапрос ссылки после ошибки (resumeAt > 0) — то же прослушивание.
    if (resumeAt === 0) playReported = scrobbled = false;
    if (audioContext?.state === 'suspended') {
      try {
        await audioContext.resume();
      } catch {
        /* игнорируем — play() ниже всё равно попытается */
      }
      if (seq !== loadSeq) return;
    }
    cancelPendingCrossfade();

    state.update((s) => ({ ...s, isLoading: true, error: null }));
    hooks.onTrackStart?.(track); // не ждёт ссылки: поиск нескачанного трека — секунды
    let source;
    try {
      source = await resolveSource(track);
    } catch (e) {
      if (seq !== loadSeq) return;
      console.error('stream link failed', e);
      elements[activeSlot].pause(); // показан уже новый трек — прежний не должен звучать под ним
      return trackFailed(track, e);
    }
    if (seq !== loadSeq) return; // пока ждали ссылку, включили другой трек
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
      // Новая загрузка сменила src, пока этот play() ждал, — так и задумано.
      if (seq !== loadSeq || e?.name === 'AbortError') return;
      // Браузер не дал играть без нажатия — трек ждёт на паузе. Файл не
      // открылся — придёт 'error', им займётся handlePlaybackError.
      state.update((x) => ({ ...x, isLoading: false }));
    }
  }

  // tracks: [{ id, title, artist?, artistHref?, album?, albumHref?, durationMs?, coverUrl? }];
  // трек не из медиатеки — { id: null, deezerId, source?: { url, gainDb } } (см. resolveSource)
  // chosen: false — включили не этот трек, а место целиком (Play альбома,
  // радио): не сыгравший трек тогда пропускается.
  async function playQueue(tracks, startIndex = 0, { chosen = true } = {}) {
    let queue = tracks;
    let index = startIndex;
    if (get(state).shuffle) {
      // Выбранный трек играет первым, остальные — в случайном порядке.
      unshuffled = tracks;
      queue = [tracks[startIndex], ...shuffled(tracks.filter((_, i) => i !== startIndex))];
      index = 0;
    } else {
      unshuffled = null;
    }
    state.update((s) => ({ ...s, queue, index }));
    await loadTrack(queue[index], 0, !chosen);
  }

  function saveModes() {
    const { shuffle, repeat } = get(state);
    localStorage.setItem(MODES_STORAGE_KEY, JSON.stringify({ shuffle, repeat }));
  }

  // toggleShuffle перемешивает всё после текущего трека (текущий остаётся
  // играть) и запоминает исходный порядок, чтобы вернуть его при выключении.
  function toggleShuffle() {
    cancelPendingCrossfade();
    const s = get(state);
    const current = s.queue[s.index];
    if (!s.shuffle) {
      unshuffled = s.queue;
      const rest = s.queue.filter((_, i) => i !== s.index);
      const queue = current ? [current, ...shuffled(rest)] : shuffled(rest);
      state.update((x) => ({ ...x, shuffle: true, queue, index: current ? 0 : -1 }));
    } else {
      const queue = unshuffled && unshuffled.length ? unshuffled : s.queue;
      const index = current ? queue.indexOf(current) : -1;
      unshuffled = null;
      state.update((x) => ({ ...x, shuffle: false, queue, index }));
    }
    saveModes();
  }

  // off → all → one → off, как в Spotify.
  function cycleRepeat() {
    cancelPendingCrossfade();
    state.update((x) => ({ ...x, repeat: x.repeat === 'off' ? 'all' : x.repeat === 'all' ? 'one' : 'off' }));
    saveModes();
  }

  function playTrack(track) {
    return playQueue([track], 0);
  }

  // В очередь — после уже добавленного руками, но перед подобранным
  // автовоспроизведением, как в Spotify: иначе трек заиграл бы только
  // после всего радио. addToQueue — один трек, appendToQueue — несколько.
  function addToQueue(track) {
    return appendToQueue([track]);
  }

  function appendToQueue(tracks) {
    if (tracks.length === 0) return Promise.resolve();
    const s = get(state);
    if (s.queue.length === 0) {
      return playQueue(tracks, 0);
    }
    const insertAt = (list, from) => {
      const i = list.findIndex((e, j) => j > from && e.autoplay);
      return i < 0 ? list.length : i;
    };
    const at = insertAt(s.queue, s.index);
    // Подготовленный к переходу трек стоял на этом месте — он больше не следующий.
    if (preloadedForIndex !== null && at <= preloadedForIndex) cancelPendingCrossfade();
    if (unshuffled) {
      const u = insertAt(unshuffled, unshuffled.indexOf(s.queue[s.index]));
      unshuffled = [...unshuffled.slice(0, u), ...tracks, ...unshuffled.slice(u)];
    }
    state.update((x) => ({ ...x, queue: [...x.queue.slice(0, at), ...tracks, ...x.queue.slice(at)] }));
    return Promise.resolve();
  }

  // «Играть следующим» — сразу после текущего трека, но после того, что уже
  // поставили следующим раньше (по порядку добавления).
  function playNext(track) {
    const s = get(state);
    if (s.queue.length === 0) {
      return playQueue([track], 0);
    }
    const entry = { ...track, upNext: true };
    const insertAt = (list, from) => {
      let i = from + 1;
      while (i < list.length && list[i].upNext) i++;
      return i;
    };
    const at = insertAt(s.queue, s.index);
    if (preloadedForIndex !== null && at <= preloadedForIndex) cancelPendingCrossfade();
    if (unshuffled) {
      const u = insertAt(unshuffled, unshuffled.indexOf(s.queue[s.index]));
      unshuffled = [...unshuffled.slice(0, u), entry, ...unshuffled.slice(u)];
    }
    state.update((x) => ({ ...x, queue: [...x.queue.slice(0, at), entry, ...x.queue.slice(at)] }));
    return Promise.resolve();
  }

  async function removeFromQueue(idx) {
    const s = get(state);
    if (idx < 0 || idx >= s.queue.length) return;
    cancelPendingCrossfade();

    const newQueue = s.queue.filter((_, i) => i !== idx);
    if (unshuffled) unshuffled = unshuffled.filter((t) => t !== s.queue[idx]);

    if (idx === s.index) {
      if (newQueue.length === 0) {
        pause();
        state.update((x) => ({ ...x, queue: [], index: -1 }));
        return;
      }
      const newIndex = Math.min(idx, newQueue.length - 1);
      state.update((x) => ({ ...x, queue: newQueue, index: newIndex }));
      await loadTrack(newQueue[newIndex], 0, true);
      return;
    }

    const newIndex = idx < s.index ? s.index - 1 : s.index;
    state.update((x) => ({ ...x, queue: newQueue, index: newIndex }));
  }

  function moveInQueue(fromIdx, direction) {
    const s = get(state);
    const toIdx = fromIdx + direction;
    if (toIdx < 0 || toIdx >= s.queue.length) return;
    cancelPendingCrossfade();

    const newQueue = [...s.queue];
    [newQueue[fromIdx], newQueue[toIdx]] = [newQueue[toIdx], newQueue[fromIdx]];

    let newIndex = s.index;
    if (s.index === fromIdx) newIndex = toIdx;
    else if (s.index === toIdx) newIndex = fromIdx;

    state.update((x) => ({ ...x, queue: newQueue, index: newIndex }));
  }

  // Перетаскивание в очереди: трек fromIdx встаёт на место toIdx.
  function moveInQueueTo(fromIdx, toIdx) {
    const s = get(state);
    const n = s.queue.length;
    if (fromIdx === toIdx || fromIdx < 0 || toIdx < 0 || fromIdx >= n || toIdx >= n) return;
    cancelPendingCrossfade();
    const newQueue = [...s.queue];
    const [moved] = newQueue.splice(fromIdx, 1);
    newQueue.splice(toIdx, 0, moved);
    const current = s.queue[s.index];
    state.update((x) => ({ ...x, queue: newQueue, index: newQueue.indexOf(current) }));
  }

  async function jumpTo(idx) {
    const s = get(state);
    if (idx < 0 || idx >= s.queue.length || idx === s.index) return;
    state.update((x) => ({ ...x, index: idx }));
    await loadTrack(s.queue[idx]);
  }

  function pause() {
    if (elements[activeSlot]) elements[activeSlot].pause();
  }

  // Выход из аккаунта: тишина и пустая очередь — следующий, кто войдёт на
  // этом устройстве, не увидит и не услышит чужого. Пустая очередь стирает
  // и сохранённую (saveQueue), в том числе на pagehide.
  function clear() {
    loadSeq++; // запоздавшая ссылка не включит трек уже после выхода
    cancelPendingCrossfade();
    for (const el of elements) el?.pause();
    setSleepTimer(null);
    unshuffled = null;
    restoredPending = false;
    state.update((s) => ({ ...s, queue: [], index: -1, isPlaying: false, isLoading: false, currentTime: 0, duration: 0, error: null }));
    if ('mediaSession' in navigator) navigator.mediaSession.metadata = null;
    setPlaybackState('none');
  }

  // Восстановленная после перезагрузки очередь: трек ещё не загружен.
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
    if (elements[activeSlot]) elements[activeSlot].play();
  }

  // toggle смотрит на сам элемент, а не на isPlaying в сторе: если они
  // когда-нибудь разойдутся, кнопка всё равно сделает то, что видно/слышно.
  function toggle() {
    if (restoredPending) return playRestored();
    const el = elements[activeSlot];
    if (!el) return;
    if (el.paused) el.play();
    else el.pause();
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

  // Как в плеерах: если трек играет дольше 3 с — сначала в его начало.
  // Кнопка «назад» после 3 с играния начинает трек сначала; свайп по
  // обложке (always) — всегда к предыдущему, как в Spotify.
  async function prev({ always = false } = {}) {
    const s = get(state);
    // Трек ещё не загружен (после перезагрузки или таймера сна) — в элементе
    // играл другой, на его позицию не смотрим.
    const el = restoredPending ? null : elements[activeSlot];
    if (el && el.currentTime > 3 && !(always && s.index > 0)) {
      el.currentTime = 0;
      return;
    }
    if (s.index > 0) await goTo(s.index - 1);
    else if (s.repeat !== 'off' && s.queue.length > 1) await goTo(s.queue.length - 1);
    else if (el) el.currentTime = 0;
  }

  // hasNext/hasPrev для состояния кнопок. С автовоспроизведением «дальше»
  // есть всегда: оно само подберёт, что играть. autoplay передают
  // компоненты ($settings.autoplay) — так кнопка обновляется сразу, как
  // только его переключили.
  function canGoNext(s, autoplay = get(settings).autoplay) {
    return followingIndex(s) !== null || (autoplay && s.repeat === 'off' && s.queue.length > 0 && !!hooks.autoplay);
  }

  function setHooks(h) {
    hooks = { ...hooks, ...h };
  }

  // Дополнить элемент очереди (например, всеми исполнителями). Элемент
  // меняется на месте — на него же ссылается исходный порядок для shuffle.
  function patchEntry(entry, fields) {
    Object.assign(entry, fields);
    const s = get(state);
    if (s.queue[s.index] === entry) updateMediaSession(entry);
    state.update((x) => ({ ...x, queue: [...x.queue] }));
  }

  function maybeReportPlay(el) {
    maybeScrobble(el);
    if (playReported || !hooks.onPlayed) return;
    const d = el.duration;
    if (!Number.isFinite(d) || d <= 0 || el.currentTime < Math.min(PLAY_REPORT_SECONDS, d / 2)) return;
    playReported = true;
    const s = get(state);
    const entry = s.queue[s.index];
    if (entry) hooks.onPlayed(entry);
  }

  // Правило Last.fm: трек длиннее 30 с, дослушан до половины или до 4 минут.
  function maybeScrobble(el) {
    if (scrobbled || !hooks.onScrobble) return;
    const d = el.duration;
    if (!Number.isFinite(d) || d <= SCROBBLE_MIN_SECONDS || el.currentTime < Math.min(SCROBBLE_MAX_SECONDS, d / 2)) return;
    scrobbled = true;
    const s = get(state);
    const entry = s.queue[s.index];
    if (entry) hooks.onScrobble(entry, Math.round(Date.now() / 1000 - el.currentTime));
  }

  function autoplayOn() {
    return get(settings).autoplay && get(state).repeat === 'off';
  }

  // Дописывает в конец очереди продолжение (элементы помечены autoplay,
  // очередь показывает их отдельно). true — что-то добавилось.
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

  // Автовоспроизведение выключили — подобранное им убираем из очереди
  // (кроме уже играющего), как Spotify.
  function dropAutoplayed() {
    const s = get(state);
    if (!s.queue.some((e, i) => e.autoplay && i > s.index)) return;
    cancelPendingCrossfade();
    const queue = s.queue.filter((e, i) => !e.autoplay || i <= s.index);
    if (unshuffled) unshuffled = unshuffled.filter((e) => queue.includes(e));
    state.update((x) => ({ ...x, queue }));
  }

  // --- таймер сна ---
  // Через N минут — пауза с коротким затуханием; «до конца трека» — после
  // текущего трека дальше не играем. Таймер живёт до перезагрузки страницы.
  const SLEEP_FADE_SECONDS = 3;
  let sleepHandle = null;

  /** minutes: число минут, 'track' — до конца трека, null — выключить. */
  function setSleepTimer(minutes) {
    clearTimeout(sleepHandle);
    sleepHandle = null;
    let sleep = null;
    if (minutes === 'track') {
      sleep = { endOfTrack: true };
      cancelPendingCrossfade(); // переход в следующий трек больше не нужен
    } else if (minutes > 0) {
      sleep = { at: Date.now() + minutes * 60000 };
      sleepHandle = setTimeout(maybeSleep, minutes * 60000);
    }
    state.update((s) => ({ ...s, sleep }));
  }

  function maybeSleep() {
    const at = get(state).sleep?.at;
    if (!at || Date.now() < at) return;
    clearTimeout(sleepHandle);
    sleepHandle = null;
    state.update((s) => ({ ...s, sleep: null }));
    const el = elements[activeSlot];
    if (!el || el.paused) return;
    cancelPendingCrossfade();
    if (directAudio) {
      el.pause(); // без Web Audio не затухает — просто пауза
      return;
    }
    const gain = gains[activeSlot];
    const now = ctxTime();
    gain.gain.cancelScheduledValues(now);
    gain.gain.setValueAtTime(gain.gain.value, now);
    gain.gain.linearRampToValueAtTime(0, now + SLEEP_FADE_SECONDS);
    setTimeout(() => {
      el.pause();
      gain.gain.cancelScheduledValues(ctxTime());
      // За время затухания трек мог смениться — тогда этот слот уже тихий.
      gain.gain.value = gain === gains[activeSlot] ? get(state).volume * trackGain(activeSlot) : 0;
    }, SLEEP_FADE_SECONDS * 1000);
  }

  // Трек доиграл при таймере «до конца трека»: следующий ждёт на паузе (как
  // после перезагрузки — первый play() его загрузит).
  function sleepAfterTrack(s) {
    cancelPendingCrossfade();
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

  // Позиция в сторе меняется сразу, а не со следующим timeupdate: иначе
  // полоса на долю секунды возвращалась на старое место.
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

  // Точная позиция для текста песни: стор обновляется 4 раза в секунду
  // (timeupdate), а подсветке слов нужна позиция на каждый кадр.
  function position() {
    const el = elements[activeSlot];
    return el && !restoredPending ? el.currentTime : get(state).currentTime;
  }

  function setVolume(v) {
    const clamped = Math.min(1, Math.max(0, v));
    if (gains[activeSlot]) gains[activeSlot].gain.value = clamped * trackGain(activeSlot);
    localStorage.setItem(VOLUME_STORAGE_KEY, String(clamped));
    state.update((s) => ({ ...s, volume: clamped }));
  }

  // Переключили выравнивание громкости — применить к играющему треку сразу.
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
export { formatTime };

/**
 * Часть состояния плеера, которая меняется редко. Сам плеер меняется
 * 4 раза в секунду (timeupdate — позиция трека), и всё, что подписано на
 * него целиком, пересчитывается на каждом тике. select выдаёт новое
 * значение (fn), только когда изменилось что-то из deps.
 */
export function select(store, deps, fn = (s) => deps(s)[0]) {
  return readable(undefined, (set) => {
    let prev = null;
    return store.subscribe((s) => {
      const d = deps(s);
      if (prev && d.every((x, i) => x === prev[i])) return;
      prev = d;
      set(fn(s));
    });
  });
}

// Текущий элемент очереди. Зависит и от массива очереди: patchEntry правит
// элемент на месте (догруженные исполнители) и заменяет только массив.
export const nowPlaying = select(player, (s) => [s.queue, s.index], (s) => s.queue[s.index] ?? null);
export const isPlaying = select(player, (s) => [s.isPlaying]);
export const shuffleOn = select(player, (s) => [s.shuffle]);
