export const RECORD_TYPES = ['single', 'ep', 'album', 'compile'];

export const AUDIO_ACCEPT = 'audio/*,.flac,.wav,.aif,.aiff,.mp3,.m4a,.mp4,.aac,.ogg,.oga,.opus,.mka,.webm';
export const MAX_AUDIO_BYTES = 200 * 1024 * 1024;
const AUDIO_EXT = /\.(flac|wav|aiff?|mp3|m4a|mp4|aac|ogg|oga|opus|mka|webm)$/i;

export const isAudioFile = (file) => file.type.startsWith('audio/') || AUDIO_EXT.test(file.name);

const HALF_HOUR = 30 * 60 * 1000;

export function recordTypeFor(tracks) {
  const n = tracks.length;
  const ms = tracks.reduce((sum, t) => sum + (t.durationMs || 0), 0);
  if (n <= 3 && ms < HALF_HOUR) return 'single';
  if (n <= 6 && ms < HALF_HOUR) return 'ep';
  return 'album';
}

export function missingForPublish(r) {
  const missing = [];
  if (!r.title?.trim()) missing.push('title');
  if (!r.artists?.length) missing.push('artists');
  if (!r.recordType) missing.push('record_type');
  if (!r.releaseDate) missing.push('release_date');
  const ready = r.rows.filter((row) => row.id);
  if (!ready.length) missing.push('tracks');
  if (ready.some((row) => !row.other && !row.title?.trim())) missing.push('track_titles');
  if (r.rows.some((row) => !row.id)) missing.push('uploading');
  return missing;
}

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

export function withSeparators(tracks, sepKey) {
  const out = [];
  tracks.forEach((tr, i) => {
    if (i > 0 && (tr.disc || 1) > (tracks[i - 1].disc || 1)) out.push({ key: sepKey(), sep: true });
    out.push(tr);
  });
  return out;
}

export const naturalCompare = (a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });

export function fullDate(tag) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(tag || '');
  if (!m) return '';
  const d = new Date(`${m[1]}-${m[2]}-${m[3]}T00:00:00Z`);
  return Number.isNaN(d.getTime()) || d.getUTCDate() !== Number(m[3]) ? '' : `${m[1]}-${m[2]}-${m[3]}`;
}

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

const NEW_ARTIST = 'new:';
export const newArtist = (name) => ({ id: NEW_ARTIST + name, name, picture_url: null });
export const isNewArtist = (a) => a.id.startsWith(NEW_ARTIST);

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
