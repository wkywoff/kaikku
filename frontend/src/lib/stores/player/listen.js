const PLAY_REPORT_SECONDS = 30;
const LISTEN_STEP_MAX_SECONDS = 5;
const LISTEN_FLUSH_SECONDS = 60;
const SCROBBLE_MIN_SECONDS = 30;
const SCROBBLE_MAX_SECONDS = 240;

export function createListenTracker(hooks, entry) {
  let playReported = false;
  let listen = null;
  let scrobbled = false;

  function currentListen() {
    if (!listen) listen = { sec: 0, lastPos: null, sent: 0, reported: false };
    return listen;
  }

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

  function tick(el) {
    trackListen(el);
    maybeScrobble(el);
    maybeReportPlay(el);
  }

  function flush() {
    const l = listen;
    if (!l?.reported || !hooks().onListened || l.sec - l.sent < 1) return;
    l.sent = l.sec;
    hooks().onListened(l, Math.round(l.sec * 1000));
  }

  function paused() {
    if (listen) listen.lastPos = null;
    flush();
  }

  function newPlay() {
    flush();
    listen = null;
    playReported = scrobbled = false;
  }

  return { tick, flush, paused, newPlay };
}
