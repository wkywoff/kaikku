// Разбор списка треков для импорта плейлиста (кроме ссылки Spotify — её
// разбирает сервер, internal/api/import.go):
//   - CSV из Exportify, TuneMyMusic и похожих: заголовок с «Track Name» и
//     «Artist Name(s)» (берём название, исполнителей и длительность);
//   - строки «Исполнитель — Трек» (дефис или тире; номера «1.» не мешают),
//     строка без тире — просто название.
// → [{ title, artists: [...], duration_ms? }], не больше MAX_IMPORT.

export const MAX_IMPORT = 300;

const SPOTIFY = /(?:open\.spotify\.com\/|spotify:)(?:intl-[a-z-]+\/)?(?:embed\/)?(playlist|album)[/:][A-Za-z0-9]{22}/i;

/** Ссылка на плейлист или альбом Spotify, если текст — она. */
export function spotifyLink(text) {
  const t = String(text || '').trim();
  return SPOTIFY.test(t) && !t.includes('\n') ? t : null;
}

export function parseTrackList(text) {
  const csv = parseCsvList(text);
  if (csv) return csv.slice(0, MAX_IMPORT);
  const out = [];
  for (let line of String(text || '').split(/\r?\n/)) {
    line = line.trim().replace(/^\d+\s*[.)]\s*/, '');
    if (!line) continue;
    const m = line.match(/^(.+?)\s+[-–—]\s+(.+)$/);
    out.push(m ? { title: m[2].trim(), artists: splitArtists(m[1]) } : { title: line, artists: [] });
    if (out.length >= MAX_IMPORT) break;
  }
  return out;
}

function splitArtists(s) {
  return s
    .split(/\s*[,;]\s*/)
    .map((a) => a.trim())
    .filter(Boolean);
}

// CSV по RFC 4180: кавычки, запятые и переводы строк внутри кавычек.
export function parseCsv(text) {
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') {
        field += '"';
        i++;
      } else if (c === '"') quoted = false;
      else field += c;
    } else if (c === '"') quoted = true;
    else if (c === ',') {
      row.push(field);
      field = '';
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += c;
  }
  if (field || row.length) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.some((f) => f.trim()));
}

function parseCsvList(text) {
  const firstLine = String(text || '').split(/\r?\n/, 1)[0].toLowerCase();
  if (!firstLine.includes('track name') || !firstLine.includes('artist name')) return null;
  const [header, ...rows] = parseCsv(text);
  const col = (re) => header.findIndex((h) => re.test(h.toLowerCase()));
  const title = col(/track name/);
  const artist = col(/artist name/);
  const duration = col(/duration/);
  return rows
    .map((r) => ({
      title: (r[title] || '').trim(),
      artists: splitArtists(r[artist] || ''),
      ...(duration >= 0 && Number(r[duration]) > 0 ? { duration_ms: Math.round(Number(r[duration])) } : {}),
    }))
    .filter((t) => t.title);
}
