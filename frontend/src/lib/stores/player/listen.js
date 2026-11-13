// Учёт прослушивания одного трека: запись в историю, сколько он реально
// играл и скроббл в Last.fm. Плеер зовёт tick на каждый timeupdate, а
// newPlay — когда началось новое прослушивание (другой трек, повтор).

// Прослушивание записывается в историю, когда трек проиграл столько секунд
// (короткий — половину), как у Spotify.
const PLAY_REPORT_SECONDS = 30;
// Сколько трек реально играл (минуты в итогах года): шаг позиции длиннее
// LISTEN_STEP_MAX_SECONDS — перемотка, не в счёт. Кроме паузы, смены трека и
// ухода со страницы, время отправляется каждые LISTEN_FLUSH_SECONDS —
// телефон может выгрузить вкладку в фоне молча.
const LISTEN_STEP_MAX_SECONDS = 5;
const LISTEN_FLUSH_SECONDS = 60;
const SCROBBLE_MIN_SECONDS = 30;
const SCROBBLE_MAX_SECONDS = 240;

/**
 * hooks() — хуки плеера (onPlayed, onListened, onScrobble; см. player.js),
 * entry() — текущий элемент очереди.
 */
export function createListenTracker(hooks, entry) {
  let playReported = false; // за текущее проигрывание трека уже записали
  let listen = null; // текущее прослушивание: { sec, lastPos, sent, reported }
  let scrobbled = false; // и отправили в Last.fm

  function currentListen() {
    if (!listen) listen = { sec: 0, lastPos: null, sent: 0, reported: false };
    return listen;
  }

  // Время игры копится по шагам позиции между timeupdate: перемотка
  // (большой шаг или назад) и пауза не в счёт.
  function trackListen(el) {
    const l = currentListen();
    if (el.paused) {
      l.lastPos = null;
      return;
    }
    const step = l.lastPos === null ? 0 : el.currentTime - l.lastPos;
    if (step > 0 && step < LISTEN_STEP_MAX_SECONDS) l.sec += step;
    l.lastPos = el.currentTime;
    if (l.sec - l.sent >= LISTEN_FLUSH_SECONDS) flush();
  }

  // Правило Last.fm: трек длиннее 30 с, дослушан до половины или до 4 минут.
  function maybeScrobble(el) {
    if (scrobbled || !hooks().onScrobble) return;
    const d = el.duration;
    if (!Number.isFinite(d) || d <= SCROBBLE_MIN_SECONDS || el.currentTime < Math.min(SCROBBLE_MAX_SECONDS, d / 2)) return;
    scrobbled = true;
    const e = entry();
    if (e) hooks().onScrobble(e, Math.round(Date.now() / 1000 - el.currentTime));
  }

  function maybeReportPlay(el) {
    if (playReported || !hooks().onPlayed) return;
    const d = el.duration;
    if (!Number.isFinite(d) || d <= 0 || el.currentTime < Math.min(PLAY_REPORT_SECONDS, d / 2)) return;
    playReported = true;
    const e = entry();
    if (!e) return;
    const play = currentListen();
    play.reported = true;
    hooks().onPlayed(e, play);
  }

  /** timeupdate играющего элемента. */
  function tick(el) {
    trackListen(el);
    maybeScrobble(el);
    maybeReportPlay(el);
  }

  /** Отправить накопленное время (пауза, уход со страницы). */
  function flush() {
    const l = listen;
    if (!l?.reported || !hooks().onListened || l.sec - l.sent < 1) return;
    l.sent = l.sec;
    hooks().onListened(l, Math.round(l.sec * 1000));
  }

  /** Пауза: время до следующего timeupdate не в счёт. */
  function paused() {
    if (listen) listen.lastPos = null;
    flush();
  }

  /**
   * Прежнее прослушивание закончилось (другой трек, повтор): отправить его
   * время и начать считать заново.
   */
  function newPlay() {
    flush();
    listen = null;
    playReported = scrobbled = false;
  }

  return { tick, flush, paused, newPlay };
}
