// Загрузка своих релизов (окно /upload/<id>): то, что не зависит от
// страницы, — тип релиза по трекам, чего не хватает для публикации, что
// заполнить из тегов файла, очередь загрузки файлов.

export const RECORD_TYPES = ['single', 'ep', 'album', 'compile'];

// Аудио, которое принимает сервер (uploads_audio.go), и его предел.
export const AUDIO_ACCEPT = 'audio/*,.flac,.wav,.aif,.aiff,.mp3,.m4a,.mp4,.aac,.ogg,.oga,.opus,.mka,.webm';
export const MAX_AUDIO_BYTES = 200 * 1024 * 1024;
const AUDIO_EXT = /\.(flac|wav|aiff?|mp3|m4a|mp4|aac|ogg|oga|opus|mka|webm)$/i;

/** Похоже ли на аудиофайл (по типу или расширению — у FLAC тип бывает пустой). */
export const isAudioFile = (file) => file.type.startsWith('audio/') || AUDIO_EXT.test(file.name);

const HALF_HOUR = 30 * 60 * 1000;

/**
 * Тип релиза по трекам, как у Spotify: 1–3 трека и меньше получаса —
 * сингл, 4–6 и меньше получаса — EP, иначе альбом.
 */
export function recordTypeFor(tracks) {
  const n = tracks.length;
  const ms = tracks.reduce((sum, t) => sum + (t.durationMs || 0), 0);
  if (n <= 3 && ms < HALF_HOUR) return 'single';
  if (n <= 6 && ms < HALF_HOUR) return 'ep';
  return 'album';
}

/**
 * Чего не хватает для публикации — те же коды, что у сервера
 * (missingForPublish в uploads.go), и ещё «файлы не догрузились».
 * r: { title, artists, recordType, releaseDate, coverUrl, rows }.
 */
export function missingForPublish(r) {
  const missing = [];
  if (!r.title?.trim()) missing.push('title');
  if (!r.artists?.length) missing.push('artists');
  if (!r.recordType) missing.push('record_type');
  if (!r.releaseDate) missing.push('release_date');
  if (!r.coverUrl) missing.push('cover');
  const ready = r.rows.filter((row) => row.id);
  if (!ready.length) missing.push('tracks');
  if (ready.some((row) => !row.other && !row.title?.trim())) missing.push('track_titles'); // у добавленных с других релизов — свои
  if (r.rows.some((row) => !row.id)) missing.push('uploading');
  return missing;
}

/**
 * Диски в списке треков окна загрузки: строка-разделитель ({ sep: true })
 * открывает следующий диск. Пустой диск не считается — разделитель в самом
 * начале или два подряд номер не сдвигают. → [{ row, disc, n }] для строк
 * треков, n — номер на своём диске.
 */
export function discsOf(rows) {
  const out = [];
  let disc = 1;
  let n = 0;
  for (const row of rows) {
    if (row.sep) {
      if (n) (disc += 1), (n = 0);
      continue;
    }
    out.push({ row, disc, n: ++n });
  }
  return out;
}

/** Строки из треков с номерами дисков (ответ сервера): перед каждым следующим диском — разделитель. */
export function withSeparators(tracks, sepKey) {
  const out = [];
  tracks.forEach((tr, i) => {
    if (i > 0 && (tr.disc || 1) > (tracks[i - 1].disc || 1)) out.push({ key: sepKey(), sep: true });
    out.push(tr);
  });
  return out;
}

/** Порядок файлов «как в папке»: 2 раньше 10. */
export const naturalCompare = (a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });

/** Полная дата из тега (YYYY-MM-DD или YYYY-MM-DDThh…); год сам по себе — не дата. */
export function fullDate(tag) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(tag || '');
  if (!m) return '';
  const d = new Date(`${m[1]}-${m[2]}-${m[3]}T00:00:00Z`);
  return Number.isNaN(d.getTime()) || d.getUTCDate() !== Number(m[3]) ? '' : `${m[1]}-${m[2]}-${m[3]}`;
}

/**
 * Что взять из тегов загруженного файла: только в пустые поля и в дату,
 * которую человек ещё не трогал (touched.releaseDate — менял сам или она
 * уже взята из другого файла). → { title?, releaseDate?, artistQuery? }
 */
export function prefillFromTags(form, tags, touched = {}) {
  const out = {};
  if (!tags) return out;
  if (!form.title?.trim() && !touched.title && tags.album) out.title = tags.album;
  const date = fullDate(tags.date);
  if (date && !touched.releaseDate) out.releaseDate = date;
  const who = tags.album_artist || tags.artist;
  if (!form.artists?.length && who) out.artistQuery = who;
  return out;
}

/**
 * Очередь загрузки файлов — по одному: сервер обрабатывает их тоже по
 * одному, а так первый трек готов раньше. run(item, onProgress, signal)
 * загружает; onChange() — состояние элементов поменялось. Занятый сервер
 * (503) — повтор через Retry-After, до maxRetries раз.
 * Элемент: { key, file, state: 'queued'|'uploading'|'processing'|'done'|'error', progress, error, result }.
 */
export function createQueue(run, onChange, { maxRetries = 5, wait = (ms) => new Promise((r) => setTimeout(r, ms)) } = {}) {
  const items = [];
  let busy = false;
  let controller = null;
  let stopped = false;

  async function next() {
    if (busy || stopped) return;
    const item = items.find((i) => i.state === 'queued');
    if (!item) return;
    busy = true;
    controller = new AbortController();
    item.state = 'uploading';
    item.progress = 0;
    item.error = '';
    onChange();
    for (let attempt = 0; ; attempt++) {
      try {
        item.result = await run(
          item,
          (p) => {
            item.progress = p;
            if (p >= 1) item.state = 'processing';
            onChange();
          },
          controller.signal,
        );
        item.state = 'done';
        break;
      } catch (e) {
        if (stopped || e?.name === 'AbortError') {
          item.state = 'error';
          item.error = '';
          break;
        }
        if (e?.status === 503 && attempt < maxRetries) {
          item.state = 'uploading';
          onChange();
          await wait((e.retryAfter || 10) * 1000);
          continue;
        }
        item.state = 'error';
        item.error = e?.message || String(e);
        break;
      }
    }
    busy = false;
    controller = null;
    onChange();
    next();
  }

  return {
    items,
    add(files) {
      for (const file of files) items.push({ key: `f${items.length}-${file.name}`, file, state: 'queued', progress: 0, error: '', result: null });
      onChange();
      next();
    },
    retry(item) {
      if (item.state !== 'error') return;
      item.state = 'queued';
      onChange();
      next();
    },
    get pending() {
      return items.some((i) => i.state === 'queued' || i.state === 'uploading' || i.state === 'processing');
    },
    stop() {
      stopped = true;
      controller?.abort();
    },
  };
}

// Новый артист из ArtistPicker — пока только имя (ID `new:<имя>`): на
// сервере его заводят при сохранении, а не по клику «Создать артиста»,
// чтобы брошенный выбор не оставлял лишних артистов.
const NEW_ARTIST = 'new:';
export const newArtist = (name) => ({ id: NEW_ARTIST + name, name, picture_url: null });
export const isNewArtist = (a) => a.id.startsWith(NEW_ARTIST);

/**
 * Заводит новых артистов из списков (релиз, треки; null пропускаются) через
 * create(name) → { id, name, picture_url } и возвращает списки с ними.
 * Один и тот же новый артист в разных списках заводится один раз.
 */
export async function createNewArtists(lists, create) {
  const made = new Map();
  for (const a of lists.flatMap((list) => list || [])) {
    if (isNewArtist(a) && !made.has(a.id)) {
      const got = await create(a.name);
      made.set(a.id, { id: String(got.id), name: got.name, picture_url: got.picture_url || null });
    }
  }
  return lists.map((list) => list && list.map((a) => made.get(a.id) || a));
}
