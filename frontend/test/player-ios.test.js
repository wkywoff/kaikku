import { test } from 'node:test';
import assert from 'node:assert/strict';

// Плеер на iPhone (directAudio): без Web Audio, один элемент <audio>, без
// предзагрузки и кроссфейда. Отдельный файл — node --test запускает его
// отдельным процессом, и player.js видит здесь «айфон».

class FakeAudio {
  constructor() {
    FakeAudio.instances.push(this);
    this.src = '';
    this.currentTime = 0;
    this.duration = NaN;
    this.volume = 1;
    this._listeners = {};
    this.playCalls = 0;
    this.paused = true;
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
    this.paused = false;
    this.dispatch('play');
    return Promise.resolve();
  }
  pause() {
    this.paused = true;
    this.dispatch('pause');
  }
  finish() {
    this.paused = true;
    this.dispatch('pause');
    this.dispatch('ended');
  }
  load() {}
  removeAttribute() {}
}
FakeAudio.instances = [];

const storage = new Map();
global.localStorage = {
  getItem: (k) => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
  removeItem: (k) => storage.delete(k),
};
global.Audio = FakeAudio;
let contexts = 0;
global.window = {
  AudioContext: class {
    constructor() {
      contexts++;
    }
  },
  addEventListener() {},
};
Object.defineProperty(global, 'navigator', {
  value: { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' },
  configurable: true,
});
global.MediaMetadata = function (data) {
  return data;
};
global.fetch = async (url) => ({ headers: new Headers(), ok: true, status: 200, json: async () => ({ url: 'https://fake/' + url, gain_db: -6 }) });

const { player, directAudio } = await import('../src/lib/stores/player.js');
const { settings } = await import('../src/lib/stores/settings.js');
settings.set('crossfade', true); // включён в настройках — на iPhone всё равно без него

test('on iPhone the player plays straight from <audio>: no Web Audio, no preload, one element', async () => {
  assert.equal(directAudio, true);
  await player.playQueue([{ id: 'a1', title: 'A1' }, { id: 'a2', title: 'A2' }], 0);
  assert.equal(contexts, 0, 'Web Audio не создаётся');
  const [el, other] = FakeAudio.instances;
  assert.ok(el.src.includes('a1'));

  el.duration = 200;
  el.currentTime = 196; // раньше здесь грузился следующий трек и начинался кроссфейд
  el.dispatch('timeupdate');
  await new Promise((r) => setTimeout(r, 10));
  assert.equal(other.src, '', 'следующий трек заранее не грузится');
  assert.equal(other.playCalls, 0);

  el.finish();
  await new Promise((r) => setTimeout(r, 10));
  assert.ok(el.src.includes('a2'), 'следующий трек — в том же элементе');
  assert.equal(el.playCalls, 2);
  assert.equal(other.playCalls, 0);
  assert.equal(el.volume, 1, 'громкость полная');
});
