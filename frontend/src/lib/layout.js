// Раскладка как у Spotify на широком экране: содержимое — колонкой не шире
// CONTENT_MAX (с боковыми отступами) по центру, в ряду карточек не больше
// MAX_PER_ROW. Сетки: repeat(auto-fill, minmax(max(176px, 10%), 1fr)) —
// столько же в CSS.
export const CONTENT_MAX = 1955; // px
export const MAX_PER_ROW = 10;

export function trackCount(gridTemplateColumns) {
  const tracks = String(gridTemplateColumns || '').trim().split(/\s+/);
  return tracks.every((t) => /^\d+(\.\d+)?px$/.test(t)) ? tracks.length : 0;
}

export function cardsPerRow(grid) {
  const columns = grid ? trackCount(getComputedStyle(grid).gridTemplateColumns) : 0;
  return Math.min(MAX_PER_ROW, columns || 2);
}
