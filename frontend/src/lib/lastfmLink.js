// Привязка Last.fm: токен в /lastfm?token= принимается, только если этот браузер
// сам недавно ушёл на Last.fm. Иначе чужая ссылка молча привязала бы к странице
// чужой аккаунт: туда пошла бы история прослушиваний.
const KEY = 'lastfmLinkStarted';
const WINDOW_MS = 30 * 60 * 1000;

export function markLastfmLink(storage = localStorage, now = Date.now()) {
  try {
    storage.setItem(KEY, String(now));
  } catch {
    // нет хранилища: привязка просто не сработает
  }
}

export function takeLastfmLink(storage = localStorage, now = Date.now()) {
  try {
    const started = Number(storage.getItem(KEY));
    storage.removeItem(KEY);
    return started > 0 && now - started >= 0 && now - started <= WINDOW_MS;
  } catch {
    return false;
  }
}
