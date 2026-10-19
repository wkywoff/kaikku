import { test } from 'node:test';
import assert from 'node:assert/strict';

// --- Минимальные стабы браузерного окружения, до импорта player.js ---

class FakeAudio {
  constructor() {
    FakeAudio.instances.push(this);
    this._src = '';
    this.currentTime = 0;
    this.duration = NaN;
    this.preload = '';
    this._listeners = {};
    this.playCalls = 0;
    this.currentTimeAtPlay = null;
    this.paused = true;
    this.ended = false;
  }
  set src(v) {
    this._src = v;
  }
  get src() {
    return this._src;
  }
  addEventListener(type, fn) {
    (this._listeners[type] ||= []).push(fn);
  }
  removeEventListener(type, fn) {
    this._listeners[type] = (this._listeners[type] || []).filter((f) => f !== fn);
  }
  dispatch(type) {
    for (const fn of this._listeners[type] || []) fn();
  }
  play() {
    this.playCalls++;
    this.currentTimeAtPlay = this.currentTime;
    this.paused = false;
    this.ended = false;
    this.dispatch('play');
    return Promise.resolve();
  }
  pause() {
    this.paused = true;
    this.dispatch('pause');
  }
  // Как в браузере: на естественном конце сначала 'pause', потом 'ended'.
  finish() {
    this.paused = true;
    this.ended = true;
    this.dispatch('pause');
    this.dispatch('ended');
  }
  load() {}
  removeAttribute() {}
}
FakeAudio.instances = [];

function fakeNode() {
  return { connect: (dest) => dest };
}

class FakeAudioContext {
  constructor() {
    this.destination = fakeNode();
    this.currentTime = 0;
    this.state = 'running';
    this.gainCalls = []; // для проверки, что фейд реально планировался
    this.gainNodes = [];
    FakeAudioContext.last = this;
  }
  createMediaElementSource() {
    return fakeNode();
  }
  createGain() {
    const node = fakeNode();
    const calls = this.gainCalls;
    this.gainNodes.push(node);
    node.gain = {
      value: 1,
      setValueAtTime(v, t) {
        calls.push(['setValueAtTime', v, t]);
      },
      linearRampToValueAtTime(v, t) {
        calls.push(['linearRampToValueAtTime', v, t]);
      },
      cancelScheduledValues(t) {
        calls.push(['cancelScheduledValues', t]);
      },
    };
    return node;
  }
  resume() {
    return Promise.resolve();
  }
}

const storage = new Map();
global.localStorage = {
  getItem: (k) => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
  removeItem: (k) => storage.delete(k),
};
global.Audio = FakeAudio;
const windowListeners = {};
global.window = {
  AudioContext: FakeAudioContext,
  addEventListener: (type, fn) => (windowListeners[type] ||= []).push(fn),
};
// Media Session: что плеер сообщает экрану блокировки.
const mediaSession = { metadata: null, playbackState: 'none', setActionHandler() {}, setPositionState() {} };
Object.defineProperty(global, 'navigator', { value: { mediaSession }, configurable: true });
global.MediaMetadata = function (data) {
  return data;
};

const streamedUrls = [];
// Треки не из медиатеки (/stream/deezer/{id}/url): пока deezerLibrary[id]
// нет — ссылка на поток и job; когда есть — уже медиатека.
const deezerLibrary = {};
const jobs = {}; // job_id -> { status, track_id? }
const radioTracks = {}; // артист -> треки его радио (/discover/artists/{id}/radio)
// Ответ на адрес с этим кусочком ждёт, пока тест не отпустит обещание.
const held = new Map();
// Deezer ID, для которых сервер не может дать ссылку (502: Deezer занят и т.п.).
const failingLinks = new Set();
// Deezer ID, которых нет на YouTube (404 с reason not_found).
const notFoundLinks = new Set();
const fakeFetch = async (url) => {
  for (const [part, gate] of held) if (url.includes(part)) await gate;
  const json = (data) => ({ ok: true, status: 200, json: async () => data });
  const deezer = url.match(/\/stream\/deezer\/(\w+)\/url$/);
  if (deezer) {
    streamedUrls.push(url);
    const id = deezer[1];
    if (failingLinks.has(id)) return { ok: false, status: 502, json: async () => ({ error: 'ingest failed: deezer busy' }) };
    if (notFoundLinks.has(id)) return { ok: false, status: 404, json: async () => ({ error: 'not found on YouTube', reason: 'not_found' }) };
    if (deezerLibrary[id]) {
      return json({ status: 'duplicate', track_id: deezerLibrary[id], url: `https://fake/media/${id}.m4a`, gain_db: -2 });
    }
    jobs[id] ||= { status: 'processing' };
    return json({ status: 'queued', job_id: id, url: `/api/stream/deezer/${id}?st=sig&e=1`, gain_db: 0 });
  }
  const radio = url.match(/\/discover\/artists\/(\w+)\/radio$/);
  if (radio && radioTracks[radio[1]]) return json({ tracks: radioTracks[radio[1]] });
  const job = url.match(/\/ingest\/(\w+)$/);
  if (job && jobs[job[1]]) return json({ job_id: job[1], ...jobs[job[1]] });
  if (url.includes('/stream-url')) {
    streamedUrls.push(url);
    // Файл трека удалён на сервере как неслушаемый.
    if (url.includes('evicted')) return { ok: false, status: 404, json: async () => ({ error: 'track not found' }) };
    // Трек с "loud" в id приходит с поправкой -6 дБ.
    const gain_db = url.includes('loud') ? -6 : 0;
    return { ok: true, status: 200, json: async () => ({ url: 'https://fake/' + url, gain_db }) };
  }
  return { ok: false, status: 404, json: async () => ({ error: 'not found' }) };
};
// Заголовки — как у настоящего ответа (api.js читает X-Auth-Token).
global.fetch = async (url) => ({ headers: new Headers(), ...(await fakeFetch(url)) });

const { player } = await import('../src/lib/stores/player.js');
const { settings } = await import('../src/lib/stores/settings.js');
settings.set('crossfade', true);
settings.set('crossfadeSeconds', 4);

test('playQueue starts the first track and requests a signed URL', async () => {
  streamedUrls.length = 0;
  const tracks = [
    { id: 'track1', title: 'One', durationMs: 200000 },
    { id: 'track2', title: 'Two', durationMs: 200000 },
  ];
  await player.playQueue(tracks, 0);

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0);
  assert.equal(s.queue.length, 2);
  assert.ok(streamedUrls.some((u) => u.includes('track1')));
});

test('manual next() is immediate — no crossfade bookkeeping involved', async () => {
  const tracks = [
    { id: 'x1', title: 'X1' },
    { id: 'x2', title: 'X2' },
  ];
  await player.playQueue(tracks, 0);
  await player.next();

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 1);
});

test('setVolume clamps to [0, 1] and persists to localStorage', () => {
  player.setVolume(1.5);
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.volume, 1);

  player.setVolume(-0.3);
  player.subscribe((v) => (s = v))();
  assert.equal(s.volume, 0);

  assert.equal(localStorage.getItem('kaikku_volume'), '0');
});

test('removeFromQueue on the currently playing track advances or stops cleanly', async () => {
  const tracks = [
    { id: 'r1', title: 'R1' },
    { id: 'r2', title: 'R2' },
  ];
  await player.playQueue(tracks, 0);
  await player.removeFromQueue(0);

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.queue.length, 1);
  assert.equal(s.queue[0].id, 'r2');
  assert.equal(s.index, 0);
});

test('removeFromQueue removing everything stops playback cleanly', async () => {
  const tracks = [{ id: 'solo', title: 'Solo' }];
  await player.playQueue(tracks, 0);
  await player.removeFromQueue(0);

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.queue.length, 0);
  assert.equal(s.index, -1);
});

test('moveInQueue swaps two tracks and keeps index pointing at the same playing track', async () => {
  const tracks = [
    { id: 'm1', title: 'M1' },
    { id: 'm2', title: 'M2' },
  ];
  await player.playQueue(tracks, 0); // играет m1 на index 0
  player.moveInQueue(0, 1); // переставить m1 на позицию 1

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.queue[0].id, 'm2');
  assert.equal(s.queue[1].id, 'm1');
  assert.equal(s.index, 1); // index должен был последовать за m1
});

test('moveInQueueTo drags a track to another place and keeps the playing one current', async () => {
  const tracks = ['d0', 'd1', 'd2', 'd3'].map((id) => ({ id, title: id }));
  await player.playQueue(tracks, 1); // играет d1
  player.moveInQueueTo(3, 2); // d3 — перед d2
  let s;
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((t) => t.id), ['d0', 'd1', 'd3', 'd2']);
  assert.equal(s.index, 1);
  player.moveInQueueTo(0, 3); // уже прослушанный — в конец: играющий сдвигается
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((t) => t.id), ['d1', 'd3', 'd2', 'd0']);
  assert.equal(s.queue[s.index].id, 'd1');
});

test('volume normalization is off by default: gain_db is ignored until enabled', async () => {
  settings.set('normalize', false);
  player.setVolume(0.5);
  await player.playQueue([{ id: 'loud0', title: 'Loud' }], 0);
  assert.equal(FakeAudioContext.last.gainNodes[0].gain.value, 0.5, 'громкость оригинала');
});

test('track gain_db scales the slot gain together with the volume', async () => {
  settings.set('normalize', true);
  player.setVolume(0.5);
  await player.playQueue([{ id: 'loud1', title: 'Loud' }], 0);
  const gain = FakeAudioContext.last.gainNodes[0].gain.value; // активный слот 0 до последнего теста
  assert.ok(Math.abs(gain - 0.5 * Math.pow(10, -6 / 20)) < 1e-9, `gain = ${gain}`);

  player.setVolume(1);
  assert.ok(Math.abs(FakeAudioContext.last.gainNodes[0].gain.value - Math.pow(10, -6 / 20)) < 1e-9);

  settings.set('normalize', false); // выключили посреди трека — сразу громкость оригинала
  assert.equal(FakeAudioContext.last.gainNodes[0].gain.value, 1);
});

test('a track that keeps failing is retried once, not in an endless loop', async () => {
  const tracks = [{ id: 'broken', title: 'Broken' }];
  await player.playQueue(tracks, 0);
  const el = FakeAudio.instances[0];
  const fetchesFor = () => streamedUrls.filter((u) => u.includes('broken')).length;
  const before = fetchesFor();

  // Каждая новая ссылка снова даёт error (битый файл / 403 от nginx).
  for (let i = 0; i < 5; i++) {
    el.dispatch('error');
    await new Promise((r) => setTimeout(r, 0));
  }

  assert.equal(fetchesFor() - before, 1, 'ровно одна попытка перевыпустить ссылку');
  let s;
  player.subscribe((v) => (s = v))();
  assert.ok(s.error, 'после неудачного повтора показывается ошибка');

  // Если трек реально заиграл, следующая ошибка (например, TTL ссылки
  // истёк на паузе) снова заслуживает повтора.
  el.dispatch('playing');
  el.dispatch('error');
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(fetchesFor() - before, 2);
});

test('shuffle keeps the current track playing and restores the original order', async () => {
  const tracks = ['s1', 's2', 's3', 's4', 's5'].map((id) => ({ id, title: id }));
  await player.playQueue(tracks, 2);
  let s;
  player.toggleShuffle();
  player.subscribe((v) => (s = v))();
  assert.equal(s.shuffle, true);
  assert.equal(s.queue[s.index].id, 's3', 'играющий трек не меняется');
  assert.deepEqual([...s.queue.map((t) => t.id)].sort(), ['s1', 's2', 's3', 's4', 's5']);

  player.toggleShuffle();
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((t) => t.id), ['s1', 's2', 's3', 's4', 's5']);
  assert.equal(s.index, 2);
});

test('repeat all wraps next() to the start; repeat one restarts on end', async () => {
  const tracks = [{ id: 'w1', title: 'W1' }, { id: 'w2', title: 'W2' }];
  await player.playQueue(tracks, 1);
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(player.canGoNext(s), false, 'без повтора после последнего ничего нет');

  player.cycleRepeat(); // all
  await player.next();
  player.subscribe((v) => (s = v))();
  assert.equal(s.repeat, 'all');
  assert.equal(s.index, 0);

  player.cycleRepeat(); // one
  const el = FakeAudio.instances[0];
  el.currentTime = 120;
  const plays = el.playCalls;
  el.finish();
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0, 'повтор одного трека не переключает трек');
  assert.equal(el.currentTime, 0);
  assert.equal(el.playCalls, plays + 1);

  player.cycleRepeat(); // off
  player.subscribe((v) => (s = v))();
  assert.equal(s.repeat, 'off');
});

test('prev() restarts the track after 3 s, otherwise goes back', async () => {
  const tracks = [{ id: 'p1', title: 'P1' }, { id: 'p2', title: 'P2' }];
  await player.playQueue(tracks, 1);
  const el = FakeAudio.instances[0];
  el.currentTime = 42;
  await player.prev();
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 1);
  assert.equal(el.currentTime, 0);
  await player.prev();
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0);
});

test('a track not in the library plays from the stream link right away', async () => {
  streamedUrls.length = 0;
  const el = FakeAudio.instances[0];
  const entry = { id: null, deezerId: 'dz1', title: 'New', source: { url: '/api/stream/deezer/dz1?st=a&e=1', gainDb: 0 } };
  await player.playQueue([entry], 0);
  assert.equal(el.src, '/api/stream/deezer/dz1?st=a&e=1', 'ссылка из клика — без лишних запросов');
  assert.equal(streamedUrls.length, 0);

  // Ошибка (например, ссылка протухла на паузе) — новая ссылка через сервер,
  // по Deezer ID: трек ещё качается — снова поток.
  el.dispatch('error');
  await new Promise((r) => setTimeout(r, 0));
  assert.ok(streamedUrls.some((u) => u.endsWith('/stream/deezer/dz1/url')));
  assert.equal(el.src, '/api/stream/deezer/dz1?st=sig&e=1');

  // Скачался — сервер даёт ссылку на медиатеку и id, дальше трек как обычный.
  deezerLibrary.dz1 = 'lib-dz1';
  el.dispatch('playing');
  el.dispatch('error');
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(el.src, 'https://fake/media/dz1.m4a');
  assert.equal(entry.id, 'lib-dz1');
});

test('playRows queues the whole list: undownloaded tracks stream, downloaded ones play from the library', async () => {
  const { playRows, isCurrentRow } = await import('../src/lib/library.js');
  streamedUrls.length = 0;
  const el = FakeAudio.instances[0];
  const rows = [
    { deezerId: 'dz2', title: 'Fresh', artists: [{ name: 'Someone' }], durationMs: 180000 },
    { libraryTrackId: 'have1', deezerId: 'dz3', title: 'Old', artists: [] },
    { title: 'No source' }, // ни id, ни Deezer ID — пропускается
  ];

  await playRows(rows, 0);
  assert.equal(el.src, '/api/stream/deezer/dz2?st=sig&e=1', 'нескачанный трек сразу играет потоком');
  let s;
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((t) => t.title), ['Fresh', 'Old'], 'в очереди весь список, а не только скачанное');
  assert.ok(isCurrentRow(rows[0], s.queue[s.index]), 'строка потока подсвечена как играющая (по Deezer ID)');
  assert.ok(!isCurrentRow(rows[1], s.queue[s.index]));

  // Клик по скачанному — играет из медиатеки, нескачанный остаётся в очереди.
  streamedUrls.length = 0;
  await playRows(rows, 1);
  assert.ok(streamedUrls.some((u) => u.endsWith('/tracks/have1/stream-url')));
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 1);
  assert.equal(s.queue.length, 2);
});

// Последний тест в файле — completeTransitionTo() меняет "активный" слот,
// так что после него FakeAudio.instances[0] уже не обязательно активен;
// остальные тесты этого не предполагают, но новые тесты после этого места
// добавлять не стоит без учёта этого.
test('approaching track end triggers preload+crossfade, then ended() transitions without an extra fetch', async () => {
  streamedUrls.length = 0;
  const tracks = [
    { id: 'ca', title: 'CA' },
    { id: 'cb', title: 'CB' },
  ];
  await player.playQueue(tracks, 0);

  const activeBefore = FakeAudio.instances[0]; // activeSlot ещё не менялся ни в одном предыдущем тесте
  activeBefore.duration = 200;

  const nextEl = FakeAudio.instances[1];
  const playsBeforePreload = nextEl.playCalls;

  // За 5с до конца (<= PRELOAD_LEAD_SECONDS=8) — должна начаться предзагрузка.
  activeBefore.currentTime = 195;
  activeBefore.dispatch('timeupdate');
  await new Promise((r) => setTimeout(r, 0)); // дать fetch-стабу разрешиться

  assert.ok(streamedUrls.some((u) => u.includes('cb')), 'preload should fetch a signed URL for the next track');
  assert.equal(nextEl.playCalls, playsBeforePreload, 'предзагрузка только буферизует — без play()');
  nextEl.currentTime = 3; // браузер мог что-то сдвинуть — к фейду трек всё равно должен начаться с нуля

  // За 3с до конца (<= CROSSFADE_SECONDS=4) — должен запуститься фейд громкости.
  const fetchCountAfterPreload = streamedUrls.length;
  activeBefore.currentTime = 197;
  activeBefore.dispatch('timeupdate');

  assert.equal(nextEl.playCalls, playsBeforePreload + 1, 'следующий трек стартует ровно в момент фейда, не на предзагрузке');
  assert.equal(nextEl.currentTimeAtPlay, 0, 'и стартует с начала');

  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0, 'index не должен меняться до фактического ended()');

  // Трек реально закончился — переход должен произойти БЕЗ повторного fetch,
  // раз аудио уже готово и играет на втором слоте.
  activeBefore.finish();
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 1, 'ended() при готовом preload должен сразу переключить index');
  assert.equal(streamedUrls.length, fetchCountAfterPreload, 'переход на подготовленный трек не должен делать новый stream-url запрос');
  assert.equal(s.isPlaying, true, "'pause' перед 'ended' не должен показывать паузу — следующий трек играет");

  // Кнопка play/pause управляет новым активным треком.
  player.toggle();
  assert.equal(nextEl.paused, true, 'toggle() ставит на паузу играющий трек');
  player.subscribe((v) => (s = v))();
  assert.equal(s.isPlaying, false);
  player.toggle();
  assert.equal(nextEl.paused, false);
});

test('seek moves the shown position at once, not on the next timeupdate', async () => {
  await player.playQueue([{ id: 'sk', title: 'SK' }], 0);
  const el = FakeAudio.instances.find((e) => e.src.includes('sk'));
  el.duration = 200;
  player.seek(140);
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(el.currentTime, 140);
  assert.equal(s.currentTime, 140, 'полоса не должна на миг вернуться на старое место');
});

test('without crossfade the next track starts right at the end (gapless), with no fetch on ended()', async () => {
  settings.set('crossfade', false);
  try {
    streamedUrls.length = 0;
    await player.playQueue([{ id: 'ga', title: 'GA' }, { id: 'gb', title: 'GB' }], 0);
    const cur = FakeAudio.instances.find((e) => e.src.includes('ga'));
    const other = FakeAudio.instances.find((e) => e !== cur);
    cur.duration = 200;

    cur.currentTime = 192; // за 8 с — предзагрузка, но ещё без play()
    cur.dispatch('timeupdate');
    await new Promise((r) => setTimeout(r, 0));
    assert.ok(other.src.includes('gb'), 'следующий трек загружается заранее');
    const plays = other.playCalls;

    cur.currentTime = 199.997; // у самого конца (в фейке время стоит) — следующий трек стартует сразу
    cur.dispatch('timeupdate');
    await new Promise((r) => setTimeout(r, 10));
    assert.equal(other.playCalls, plays + 1, 'следующий трек стартует до ended(), а не после');
    assert.equal(other.currentTimeAtPlay, 0);

    const fetches = streamedUrls.length;
    cur.finish();
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 1);
    assert.equal(s.isPlaying, true);
    assert.equal(streamedUrls.length, fetches, 'на ended() ничего не догружается');
  } finally {
    settings.set('crossfade', true);
  }
});

test('without crossfade, if the timer is late, ended() still switches to the preloaded track', async () => {
  settings.set('crossfade', false);
  try {
    streamedUrls.length = 0;
    await player.playQueue([{ id: 'la', title: 'LA' }, { id: 'lb', title: 'LB' }], 0);
    const cur = FakeAudio.instances.find((e) => e.src.includes('la'));
    const other = FakeAudio.instances.find((e) => e !== cur);
    cur.duration = 200;
    cur.currentTime = 195;
    cur.dispatch('timeupdate');
    await new Promise((r) => setTimeout(r, 0));
    const fetches = streamedUrls.length;
    const plays = other.playCalls;

    cur.finish(); // таймер не успел (например, вкладка в фоне)
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(other.playCalls, plays + 1);
    assert.equal(s.index, 1);
    assert.equal(streamedUrls.length, fetches);
  } finally {
    settings.set('crossfade', true);
  }
});

test('a play is reported once the track has played 30 s (or half of a short one)', async () => {
  const played = [];
  player.setHooks({ onPlayed: (e) => played.push(e.id) });
  try {
    await player.playQueue([{ id: 'h1', title: 'H1' }, { id: 'h2', title: 'H2' }], 0);
    const el = FakeAudio.instances.find((e) => e.src.includes('h1'));
    el.duration = 200;
    el.currentTime = 10;
    el.dispatch('timeupdate');
    assert.deepEqual(played, []);
    el.currentTime = 31;
    el.dispatch('timeupdate');
    el.currentTime = 40;
    el.dispatch('timeupdate');
    assert.deepEqual(played, ['h1'], 'одно прослушивание — одна запись');

    await player.next();
    const el2 = FakeAudio.instances.find((e) => e.src.includes('h2'));
    el2.duration = 40;
    el2.currentTime = 21;
    el2.dispatch('timeupdate');
    assert.deepEqual(played, ['h1', 'h2']);
  } finally {
    player.setHooks({ onPlayed: null });
  }
});

test('the listened time counts only playback, not seeks, and goes out when the track changes', async () => {
  const sent = [];
  player.setHooks({ onPlayed: () => {}, onListened: (play, ms) => sent.push(ms) });
  try {
    await player.playQueue([{ id: 'l1', title: 'L1' }, { id: 'l2', title: 'L2' }], 0);
    const el = FakeAudio.instances.find((e) => e.src.includes('l1'));
    el.duration = 200;
    el.paused = false;
    for (let t = 0; t <= 40; t += 0.5) {
      el.currentTime = t;
      el.dispatch('timeupdate');
    }
    el.currentTime = 150; // перемотка вперёд — не слушал
    el.dispatch('timeupdate');
    for (let t = 150.5; t <= 160; t += 0.5) {
      el.currentTime = t;
      el.dispatch('timeupdate');
    }
    assert.deepEqual(sent, [], 'до конца прослушивания — ничего');
    await player.next();
    assert.deepEqual(sent, [50000]);
  } finally {
    player.setHooks({ onPlayed: null, onListened: null });
  }
});

test('a scrobble goes out at half the track or 4 minutes, never for a track of 30 s or less', async () => {
  const scrobbled = [];
  player.setHooks({ onScrobble: (e, startedAt) => scrobbled.push([e.id, startedAt]) });
  try {
    await player.playQueue([{ id: 's1', title: 'S1' }, { id: 's2', title: 'S2' }, { id: 's3', title: 'S3' }], 0);
    const el = FakeAudio.instances.find((e) => e.src.includes('s1'));
    el.duration = 600; // 10 минут: порог — 4 минуты, не половина
    el.currentTime = 239;
    el.dispatch('timeupdate');
    assert.deepEqual(scrobbled, []);
    el.currentTime = 241;
    const now = Math.round(Date.now() / 1000);
    el.dispatch('timeupdate');
    el.currentTime = 300;
    el.dispatch('timeupdate');
    assert.equal(scrobbled.length, 1, 'одно прослушивание — один скроббл');
    assert.ok(Math.abs(scrobbled[0][1] - (now - 241)) <= 1, 'время — начало трека');

    await player.next();
    const el2 = FakeAudio.instances.find((e) => e.src.includes('s2'));
    el2.duration = 30;
    el2.currentTime = 29;
    el2.dispatch('timeupdate');
    assert.equal(scrobbled.length, 1, '30 с и короче Last.fm не считает');

    await player.next();
    const el3 = FakeAudio.instances.find((e) => e.src.includes('s3'));
    el3.duration = 120;
    el3.currentTime = 61;
    el3.dispatch('timeupdate');
    assert.deepEqual(scrobbled.map(([id]) => id), ['s1', 's3']);
  } finally {
    player.setHooks({ onScrobble: null });
  }
});

test('autoplay is off by default; when on, it continues an ended queue, and switching it off drops its picks', async () => {
  const asked = [];
  player.setHooks({
    autoplay: async (last) => {
      asked.push(last.id);
      return [{ id: 'ap1', title: 'AP1' }, { id: 'ap2', title: 'AP2' }];
    },
  });
  const wait = () => new Promise((r) => setTimeout(r, 10));
  let s;
  try {
    await player.playQueue([{ id: 'q1', title: 'Q1' }], 0);
    player.subscribe((v) => (s = v))();
    assert.equal(player.canGoNext(s), false, 'по умолчанию выключено');
    FakeAudio.instances.find((e) => e.src.includes('q1')).finish();
    await wait();
    assert.deepEqual(asked, []);

    settings.set('autoplay', true);
    await player.playQueue([{ id: 'q2', title: 'Q2' }], 0);
    player.subscribe((v) => (s = v))();
    assert.equal(player.canGoNext(s), true);
    FakeAudio.instances.find((e) => e.src.includes('q2')).finish();
    await wait();
    player.subscribe((v) => (s = v))();
    assert.deepEqual(asked, ['q2']);
    assert.deepEqual(s.queue.map((e) => e.id), ['q2', 'ap1', 'ap2']);
    assert.equal(s.index, 1, 'играет первое подобранное');
    assert.equal(s.queue[1].autoplay, true);

    settings.set('autoplay', false);
    player.subscribe((v) => (s = v))();
    assert.deepEqual(s.queue.map((e) => e.id), ['q2', 'ap1'], 'играющее остаётся, остальное подобранное убирается');
  } finally {
    settings.set('autoplay', false);
    player.setHooks({ autoplay: null });
  }
});

test('the queue survives a reload: same track, paused, resumes from the saved spot', async () => {
  await player.playQueue(
    [
      { id: 'r1', title: 'R1', durationMs: 200000, source: { url: 'one-time' } },
      { id: 'r2', title: 'R2', durationMs: 180000 },
    ],
    1,
  );
  player.seek(77);
  for (const fn of windowListeners.pagehide || []) fn();

  const { player: reloaded } = await import('../src/lib/stores/player.js?reload');
  let s;
  reloaded.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((e) => e.id), ['r1', 'r2']);
  assert.equal(s.index, 1);
  assert.equal(s.currentTime, 77);
  assert.equal(s.duration, 180);
  assert.equal(s.isPlaying, false);
  assert.equal(s.queue[0].source, undefined, 'одноразовая ссылка не сохраняется');

  await reloaded.toggle();
  const el = FakeAudio.instances.findLast((e) => e.src.includes('r2'));
  el.dispatch('loadedmetadata');
  assert.equal(el.currentTime, 77, 'продолжает с того же места');
  assert.equal(el.paused, false);
});

test('patchEntry fills in a queue entry in place (e.g. all artists of the playing song)', async () => {
  let started = [];
  player.setHooks({ onTrackStart: (e) => started.push(e.id) });
  try {
    await player.playQueue([{ id: 'pe1', title: 'PE1', artist: 'A', artists: [{ name: 'A' }] }], 0);
    assert.deepEqual(started, ['pe1'], 'хук зовётся, когда трек стал текущим');
    let s;
    player.subscribe((v) => (s = v))();
    const entry = s.queue[0];
    const before = s.queue;
    player.patchEntry(entry, { artist: 'A, B', artists: [{ name: 'A' }, { name: 'B' }] });
    player.subscribe((v) => (s = v))();
    assert.notEqual(s.queue, before, 'новый массив — подписчики перерисуются');
    assert.equal(s.queue[0], entry);
    assert.equal(s.queue[0].artist, 'A, B');
  } finally {
    player.setHooks({ onTrackStart: null });
  }
});

test('"add to queue" goes after songs queued by hand but before autoplay picks', async () => {
  settings.set('autoplay', true);
  player.setHooks({ autoplay: async () => [{ id: 'auto1', title: 'A1' }, { id: 'auto2', title: 'A2' }] });
  try {
    await player.playQueue([{ id: 'base', title: 'Base' }], 0);
    await player.next(); // очередь кончилась — автовоспроизведение добавило auto1, auto2 и играет auto1
    let s;
    player.subscribe((v) => (s = v))();
    assert.deepEqual(s.queue.map((e) => e.id), ['base', 'auto1', 'auto2']);

    await player.addToQueue({ id: 'mine1', title: 'M1' });
    await player.addToQueue({ id: 'mine2', title: 'M2' });
    player.subscribe((v) => (s = v))();
    assert.deepEqual(s.queue.map((e) => e.id), ['base', 'auto1', 'mine1', 'mine2', 'auto2']);
    assert.equal(s.index, 1, 'играющий трек не меняется');
    assert.equal(player.canGoNext(s, false), true, 'дальше — своё, даже без автовоспроизведения');
  } finally {
    settings.set('autoplay', false);
    player.setHooks({ autoplay: null });
  }
});

test('playingContext tells where the playing song was started from', async () => {
  const { playRows, playingContext, isContext } = await import('../src/lib/library.js');
  const { get } = await import('svelte/store');
  await playRows([{ deezerId: '1', title: 'A' }, { deezerId: '2', title: 'B' }], 0, { kind: 'album', id: '9' });
  const ctx = get(playingContext);
  assert.equal(isContext(ctx, 'album', '9'), true);
  assert.equal(isContext(ctx, 'album', '8'), false, 'другой альбом не играет');
  assert.equal(isContext(ctx, 'playlist', '9'), false);

  await player.addToQueue({ deezerId: '3', title: 'Queued by hand' });
  await player.jumpTo(2);
  assert.equal(get(playingContext), null, 'добавленное вручную — ни из какого места');
});

test('"play next" goes right after the playing song, in the order songs were added', async () => {
  await player.playQueue([{ id: 'n1', title: 'N1' }, { id: 'n2', title: 'N2' }, { id: 'n3', title: 'N3' }], 0);
  await player.playNext({ id: 'pn1', title: 'PN1' });
  await player.playNext({ id: 'pn2', title: 'PN2' });
  await player.addToQueue({ id: 'q1', title: 'Q1' });
  let s;
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((e) => e.id), ['n1', 'pn1', 'pn2', 'n2', 'n3', 'q1']);
  assert.equal(s.index, 0);

  await player.next(); // играет pn1 — следующее «следующим» встаёт после pn2
  await player.playNext({ id: 'pn3', title: 'PN3' });
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((e) => e.id), ['n1', 'pn1', 'pn2', 'pn3', 'n2', 'n3', 'q1']);
});

test('sleep timer "end of track": the queue stops after this song and the next one waits paused', async () => {
  await player.playQueue([{ id: 'sl1', title: 'SL1' }, { id: 'sl2', title: 'SL2', durationMs: 90000 }], 0);
  const el = FakeAudio.instances.find((e) => e.src.includes('sl1'));
  const other = FakeAudio.instances.find((e) => e !== el);
  el.duration = 200;
  player.setSleepTimer('track');
  el.currentTime = 195; // в обычном режиме тут готовился бы переход
  el.dispatch('timeupdate');
  await new Promise((r) => setTimeout(r, 0));
  assert.ok(!other.src.includes('sl2'), 'следующий трек не готовится');

  el.finish();
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.sleep, null, 'таймер сработал и выключился');
  assert.equal(s.index, 1, 'следующий трек выбран');
  assert.equal(s.isPlaying, false, '…но не играет');
  assert.equal(s.duration, 90);

  await player.prev(); // позиция старого элемента не мешает — уходим к первому треку
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0);
});

test('sleep timer in minutes fades out and pauses; switching it off cancels it', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const realNow = Date.now;
  try {
    await player.playQueue([{ id: 'sm1', title: 'SM1' }], 0);
    const el = FakeAudio.instances.find((e) => e.src.includes('sm1'));
    player.setSleepTimer(15);
    let s;
    player.subscribe((v) => (s = v))();
    assert.ok(s.sleep.at > realNow() + 14 * 60000);

    player.setSleepTimer(null);
    t.mock.timers.tick(16 * 60000);
    assert.equal(el.paused, false, 'выключенный таймер ничего не делает');

    player.setSleepTimer(30);
    Date.now = () => realNow() + 31 * 60000; // время вышло, пока вкладка спала
    el.dispatch('timeupdate');
    player.subscribe((v) => (s = v))();
    assert.equal(s.sleep, null);
    assert.equal(el.paused, false, 'сначала затухание');
    t.mock.timers.tick(3000);
    assert.equal(el.paused, true, 'потом пауза');
  } finally {
    Date.now = realNow;
  }
});

test('a song played from search continues with similar songs, not the next search result', async () => {
  const { playWithSimilar } = await import('../src/lib/library.js');
  radioTracks['77'] = [
    { id: '7701', title: 'Similar 1', artist: 'X', artist_id: '78' },
    { id: '7700', title: 'The searched song again', artist: 'A', artist_id: '77' },
    { id: '7702', title: 'Similar 2', artist: 'Y', artist_id: '79' },
  ];
  await playWithSimilar({ deezerId: '7700', title: 'Searched', artists: [{ id: '77', name: 'A', href: '/artist/77' }] });
  let s;
  player.subscribe((v) => (s = v))();
  assert.deepEqual(s.queue.map((e) => e.deezerId), ['7700', '7701', '7702'], 'сам трек, потом похожие без повтора');
  assert.equal(s.index, 0);
  assert.equal(s.queue[1].autoplay, undefined, 'это не автовоспроизведение: не пропадёт, если его выключить');
});

test('the next song is prepared at least 10 s before the end, earlier for a long crossfade', async () => {
  const prepared = async (fade, remaining) => {
    settings.set('crossfade', fade > 0);
    if (fade > 0) settings.set('crossfadeSeconds', fade);
    streamedUrls.length = 0;
    await player.playQueue([{ id: `pa${fade}${remaining}`, title: 'A' }, { id: `pb${fade}${remaining}`, title: 'B' }], 0);
    const s = (() => { let v; player.subscribe((x) => (v = x))(); return v; })();
    const el = FakeAudio.instances.find((e) => e.src.includes(s.queue[0].id));
    el.duration = 200;
    el.currentTime = 200 - remaining;
    el.dispatch('timeupdate');
    await new Promise((r) => setTimeout(r, 0));
    return streamedUrls.some((u) => u.includes(`pb${fade}${remaining}`));
  };
  try {
    assert.equal(await prepared(2, 10), true, 'короткий переход — всё равно за 10 с');
    assert.equal(await prepared(2, 11), false);
    assert.equal(await prepared(12, 16), true, 'переход 12 с — за 16 с');
    assert.equal(await prepared(0, 10), true, 'без кроссфейда — за 10 с');
  } finally {
    settings.set('crossfade', true);
    settings.set('crossfadeSeconds', 4);
  }
});

test('select: subscribers hear only about their part, not every position tick', async () => {
  const { select } = await import('../src/lib/stores/player.js');
  const { writable } = await import('svelte/store');
  const entry = { title: 'A' };
  const s = writable({ queue: [entry], index: 0, isPlaying: false, currentTime: 0 });
  const now = select(s, (x) => [x.queue, x.index], (x) => x.queue[x.index] ?? null);
  const playing = select(s, (x) => [x.isPlaying]);
  const seenNow = [];
  const seenPlaying = [];
  const offNow = now.subscribe((v) => seenNow.push(v));
  const offPlaying = playing.subscribe((v) => seenPlaying.push(v));
  for (let t = 1; t <= 10; t++) s.update((x) => ({ ...x, currentTime: t })); // timeupdate
  assert.deepEqual(seenNow, [entry]);
  assert.deepEqual(seenPlaying, [false]);
  s.update((x) => ({ ...x, isPlaying: true }));
  assert.deepEqual(seenPlaying, [false, true]);
  assert.equal(seenNow.length, 1);
  // patchEntry: тот же элемент, новый массив очереди — текущий трек сообщается снова.
  s.update((x) => ({ ...x, queue: [...x.queue] }));
  assert.equal(seenNow.length, 2);
  s.update((x) => ({ ...x, index: 1 }));
  assert.deepEqual(seenNow.at(-1), null);
  offNow();
  offPlaying();
});

test('a late answer for an earlier song does not replace the song picked after it', async () => {
  // Ссылку на нескачанный трек сервер ищет секунды; за это время включили
  // другой, скачанный, — он отвечает сразу и должен остаться играть.
  let release;
  held.set('late1', new Promise((r) => (release = r)));
  try {
    const first = player.playQueue([{ id: 'late1', title: 'Late' }, { id: 'quick2', title: 'Quick' }], 0);
    await new Promise((r) => setTimeout(r, 0));
    await player.jumpTo(1);
    const el = FakeAudio.instances.find((e) => e.src.includes('quick2'));
    assert.ok(el, 'второй трек играет сразу');
    release();
    await first;
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 1);
    assert.ok(el.src.includes('quick2'), 'запоздавшая ссылка не перебивает выбранный трек');
    assert.ok(!FakeAudio.instances.some((e) => e.src.includes('late1')), 'и не играет нигде');
  } finally {
    held.delete('late1');
  }
});

const missing = (id) => ({ id: null, deezerId: id, title: id });

test('a song the server has no link for says so, stops loading, and the previous one goes quiet', async () => {
  const { toasts } = await import('../src/lib/stores/toast.js');
  failingLinks.add('nf1');
  try {
    await player.playQueue([{ id: 'okA', title: 'Fine' }], 0);
    const before = FakeAudio.instances.find((e) => e.src.includes('okA'));
    await player.playQueue([{ id: null, deezerId: 'nf1', title: 'Nowhere' }], 0);
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(s.isLoading, false, 'без вечной загрузки');
    assert.ok(s.error);
    let shown;
    toasts.subscribe((v) => (shown = v))();
    assert.equal(shown?.kind, 'error');
    assert.match(shown.text, /Nowhere/);
    assert.equal(before.paused, true, 'прежний трек не звучит под новым названием');
  } finally {
    failingLinks.delete('nf1');
  }
});

test('when the next song fails on its own, the player moves on — at most three in a row', async () => {
  for (const id of ['nf2', 'nf3', 'nf4', 'nf5', 'nf6', 'nf7']) failingLinks.add(id);
  const wait = () => new Promise((r) => setTimeout(r, 10));
  try {
    await player.playQueue([{ id: 'sA', title: 'A' }, missing('nf2'), missing('nf3'), { id: 'sD', title: 'D' }], 0);
    FakeAudio.instances.find((e) => e.src.includes('sA')).finish();
    await wait();
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 3, 'два несыгравших пропущены');
    assert.ok(FakeAudio.instances.some((e) => e.src.includes('sD') && !e.paused));

    // Ломается всё подряд (YouTube, сеть) — после трёх пропусков стоп.
    await player.playQueue([{ id: 'sB', title: 'B' }, missing('nf4'), missing('nf5'), missing('nf6'), missing('nf7'), { id: 'sE', title: 'E' }], 0);
    FakeAudio.instances.find((e) => e.src.includes('sB')).finish();
    await wait();
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 4);
    assert.equal(s.isPlaying, false);
    assert.ok(!FakeAudio.instances.some((e) => e.src.includes('sE')));
  } finally {
    for (const id of ['nf2', 'nf3', 'nf4', 'nf5', 'nf6', 'nf7']) failingLinks.delete(id);
  }
});

test('a song clicked by hand that fails stays put; Play on an album skips to what plays', async () => {
  const { playRows } = await import('../src/lib/library.js');
  failingLinks.add('nf8');
  const wait = () => new Promise((r) => setTimeout(r, 10));
  try {
    await player.playQueue([missing('nf8'), { id: 'sF', title: 'F' }], 0);
    await wait();
    let s;
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 0, 'остаётся на выбранном, с сообщением');
    assert.ok(!FakeAudio.instances.some((e) => e.src.includes('sF')));

    // Play на карточке альбома выбирает не трек, а альбом: сбойный первый пропускается.
    await playRows([{ deezerId: 'nf8', title: 'First' }, { libraryTrackId: 'sG', deezerId: 'g2', title: 'Second' }], 0, { kind: 'album', id: '5' }, { chosen: false });
    await wait();
    player.subscribe((v) => (s = v))();
    assert.equal(s.index, 1);
    assert.ok(FakeAudio.instances.some((e) => e.src.includes('sG') && !e.paused));
  } finally {
    failingLinks.delete('nf8');
  }
});

test('logging out stops the music and forgets the queue, also after a reload', async () => {
  await player.playQueue([{ id: 'lo1', title: 'LO1' }, { id: 'lo2', title: 'LO2' }], 0);
  const el = FakeAudio.instances.find((e) => e.src.includes('lo1'));
  assert.ok(localStorage.getItem('kaikku_queue'));
  player.clear();
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.queue.length, 0);
  assert.equal(s.index, -1);
  assert.equal(el.paused, true, 'аудио живёт вне страницы — его надо остановить');
  assert.equal(localStorage.getItem('kaikku_queue'), null, 'следующий вход не увидит чужую очередь');
});

test('a library song whose file the server removed plays again through Deezer', async () => {
  streamedUrls.length = 0;
  const entry = { id: 'evicted1', deezerId: 'dzEv', title: 'Old favourite' };
  await player.playQueue([entry], 0);
  assert.ok(streamedUrls.some((u) => u.endsWith('/stream/deezer/dzEv/url')), 'после 404 — поток через Deezer');
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.error, null);
  assert.equal(entry.id, null, 'старый id медиатеки забыт');
});

test('a song not found on YouTube says why it is skipped', async () => {
  const { toasts } = await import('../src/lib/stores/toast.js');
  notFoundLinks.add('nfY');
  try {
    await player.playQueue([{ id: null, deezerId: 'nfY', title: 'Rare B-side' }], 0);
    let shown;
    toasts.subscribe((v) => (shown = v))();
    assert.equal(shown?.kind, 'error');
    assert.match(shown.text, /Rare B-side/);
    assert.match(shown.text, /YouTube/);
  } finally {
    notFoundLinks.delete('nfY');
  }
});

test('the lock screen knows whether music plays and gets the cover in several sizes', async () => {
  await player.playQueue([{ id: 'ms1', title: 'Lock', coverUrl: '/media/covers/x/y.jpg' }], 0);
  const el = FakeAudio.instances.find((e) => e.src.includes('ms1'));
  el.play();
  assert.equal(mediaSession.playbackState, 'playing');
  assert.deepEqual(mediaSession.metadata.artwork.map((a) => a.sizes), ['200x200', '300x300', '640x640']);
  el.pause();
  assert.equal(mediaSession.playbackState, 'paused');
});

test('a swipe back always goes to the previous song, the button restarts after 3 s', async () => {
  await player.playQueue([{ id: 'sw1', title: 'A' }, { id: 'sw2', title: 'B' }], 1);
  const el = FakeAudio.instances.find((e) => e.src.includes('sw2'));
  el.currentTime = 10;
  await player.prev();
  let s;
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 1, 'кнопка — сначала');
  el.currentTime = 10;
  await player.prev({ always: true });
  player.subscribe((v) => (s = v))();
  assert.equal(s.index, 0, 'свайп — к предыдущему');
});
