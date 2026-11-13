// Что плеер хранит в localStorage: громкость, режимы (перемешивание,
// повтор) и очередь.

const VOLUME_STORAGE_KEY = 'kaikku_volume';
const MODES_STORAGE_KEY = 'kaikku_modes';
const QUEUE_STORAGE_KEY = 'kaikku_queue';
const QUEUE_POSITION_KEY = 'kaikku_queue_pos';

export function loadSavedModes() {
  try {
    const m = JSON.parse(localStorage.getItem(MODES_STORAGE_KEY) || '{}');
    return { shuffle: !!m.shuffle, repeat: ['off', 'all', 'one'].includes(m.repeat) ? m.repeat : 'off' };
  } catch {
    return { shuffle: false, repeat: 'off' };
  }
}

export function saveModes({ shuffle, repeat }) {
  localStorage.setItem(MODES_STORAGE_KEY, JSON.stringify({ shuffle, repeat }));
}

// Очередь переживает перезагрузку страницы: треки, какой играл и с какого
// места (как Spotify, трек ждёт на паузе). source — одноразовая ссылка, её
// не сохраняем.
//
// Сама очередь пишется, только когда меняется (saveQueue): у большого
// плейлиста это сотни килобайт JSON. Какой трек играет и с какого места —
// отдельным маленьким ключом (saveQueuePosition), его плеер обновляет раз
// в несколько секунд. rev связывает их: позиция годится только для той
// очереди, с которой записана. В самой очереди index и time тоже есть —
// на момент её записи.
let rev = null;

export function loadSavedQueue() {
  try {
    const q = JSON.parse(localStorage.getItem(QUEUE_STORAGE_KEY) || 'null');
    if (!q || !Array.isArray(q.queue) || q.queue.length === 0) return null;
    const p = q.rev ? JSON.parse(localStorage.getItem(QUEUE_POSITION_KEY) || 'null') : null;
    const at = p && p.rev === q.rev ? p : q;
    const index = Number.isInteger(at.index) && at.index >= 0 && at.index < q.queue.length ? at.index : 0;
    const order = Array.isArray(q.order) && q.order.length === q.queue.length ? q.order : null;
    return { queue: q.queue, index, time: Math.max(0, Number(at.time) || 0), order };
  } catch {
    return null;
  }
}

// s — состояние плеера; unshuffled — порядок без перемешивания (или null).
export function saveQueue(s, unshuffled) {
  try {
    if (s.queue.length === 0) {
      rev = null;
      localStorage.removeItem(QUEUE_STORAGE_KEY);
      localStorage.removeItem(QUEUE_POSITION_KEY);
      return;
    }
    const queue = s.queue.map(({ source, ...rest }) => rest);
    let order = null;
    if (unshuffled) {
      const places = new Map();
      s.queue.forEach((e, i) => {
        if (!places.has(e)) places.set(e, i);
      });
      order = unshuffled.map((e) => places.get(e) ?? -1);
    }
    rev = Math.random().toString(36).slice(2);
    localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify({ queue, index: s.index, time: s.currentTime, order, rev }));
    saveQueuePosition(s);
  } catch {
    /* переполнено или приватный режим — после перезагрузки очередь просто пустая */
  }
}

// Только какой трек и с какого места; очередь к этому времени уже записана.
export function saveQueuePosition(s) {
  if (!rev) return;
  try {
    localStorage.setItem(QUEUE_POSITION_KEY, JSON.stringify({ rev, index: s.index, time: s.currentTime }));
  } catch {
    /* см. saveQueue */
  }
}

export function loadSavedVolume() {
  const saved = localStorage.getItem(VOLUME_STORAGE_KEY);
  if (saved === null) return 1;
  const n = Number(saved);
  return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 1;
}

export function saveVolume(volume) {
  localStorage.setItem(VOLUME_STORAGE_KEY, String(volume));
}
