// Что плеер хранит в localStorage: громкость, режимы (перемешивание,
// повтор) и очередь.

const VOLUME_STORAGE_KEY = 'kaikku_volume';
const MODES_STORAGE_KEY = 'kaikku_modes';
const QUEUE_STORAGE_KEY = 'kaikku_queue';

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
export function loadSavedQueue() {
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

// s — состояние плеера; unshuffled — порядок без перемешивания (или null).
export function saveQueue(s, unshuffled) {
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

export function loadSavedVolume() {
  const saved = localStorage.getItem(VOLUME_STORAGE_KEY);
  if (saved === null) return 1;
  const n = Number(saved);
  return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 1;
}

export function saveVolume(volume) {
  localStorage.setItem(VOLUME_STORAGE_KEY, String(volume));
}
