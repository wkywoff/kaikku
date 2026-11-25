const LATEST_MONTHS = 6;

const pad = (n) => String(n).padStart(2, '0');
const isoDay = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

function monthsBefore(date, months) {
  const first = new Date(date.getFullYear(), date.getMonth() - months, 1);
  const lastDay = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
  return new Date(first.getFullYear(), first.getMonth(), Math.min(date.getDate(), lastDay));
}

export function latestRelease(albums, now = new Date()) {
  const oldest = isoDay(monthsBefore(now, LATEST_MONTHS));
  const today = isoDay(now);
  let latest = null;
  for (const a of albums) {
    const date = a.release_date || '';
    if (a.hidden || date < oldest || date > today) continue;
    if (!latest || date > latest.release_date) latest = a;
  }
  return latest;
}

export function countLiked(likedIds, ...idLists) {
  return new Set(idLists.flat().filter((id) => likedIds.has(id))).size;
}
