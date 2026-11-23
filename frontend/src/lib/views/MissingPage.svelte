<script>
  // «Не нашлось» (админ, /admin/missing): треки, которые не удалось скачать
  // — чаще всего на YouTube не нашлось подходящего видео. По каждому: дать
  // ссылку на видео (сервер скачает с него, без поиска), загрузить свой
  // файл или повторить поиск. Воркер и сам повторяет такие раз в сутки, до
  // трёх попыток (internal/ingest/housekeep.go); исчерпавшие сюда не
  // попадают — им ссылку или файл дают в правке трека (EditDialog).
  // Если YouTube не пускает сам сервер (blocked), сверху — что с этим делать:
  // прокси или куки YouTube (их загружают здесь же).
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { paths, link } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { toast } from '../stores/toast.js';
  import { sized } from '../imageproxy.js';
  import { formatTime } from '../stores/player.js';
  import { cookieUploadMessage, signInHint } from '../youtubeCookies.js';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import FileMusic from '@lucide/svelte/icons/file-music';
  import Link from '@lucide/svelte/icons/link';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import ShieldAlert from '@lucide/svelte/icons/shield-alert';
  import Cookie from '@lucide/svelte/icons/cookie';

  pageTint().none();

  const MAX_AUDIO = 200 * 1024 * 1024; // как на сервере (handlers_admin.go)
  const COOKIES_HELP = 'https://github.com/yt-dlp/yt-dlp/wiki/Extractors#exporting-youtube-cookies';

  let tracks = null;
  let blocked = false; // YouTube не пускает сервер
  let proxied = false; // YouTube уже идёт через YTDLP_PROXY
  let cookies = null; // { updated_at, count, signed_in, expires } — загруженные куки YouTube
  let cookiesBusy = ''; // 'upload' | 'remove'
  let error = '';
  let links = {}; // job_id → введённая ссылка
  let busy = {}; // job_id → 'video' | 'upload' | 'retry'
  let progress = {}; // job_id → доля загрузки файла
  let done = {}; // job_id → 'queued' | 'added': уже не «не нашлось»

  async function load() {
    try {
      ({ tracks, blocked, proxy: proxied, cookies } = await api.missing());
      // Ссылка, которую уже давали, — в поле: видно, что с ней не вышло.
      for (const m of tracks) if (m.manual_video_id) links[m.job_id] = `https://youtu.be/${m.manual_video_id}`;
    } catch (e) {
      error = e.message;
    }
  }
  load();

  async function run(m, kind, action, doneAs) {
    busy = { ...busy, [m.job_id]: kind };
    try {
      await action();
      done = { ...done, [m.job_id]: doneAs };
    } catch (e) {
      toast(e.status === 413 ? $t('edit.audioTooBig') : e.message, 'error');
    } finally {
      busy = { ...busy, [m.job_id]: null };
    }
  }

  const useVideo = (m) => run(m, 'video', () => api.setTrackVideo(m.deezer_id, links[m.job_id] || ''), 'queued');
  const retry = (m) => run(m, 'retry', () => api.retryMissing(m.job_id), 'queued');

  function upload(m, e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    if (file.size > MAX_AUDIO) return toast($t('edit.audioTooBig'), 'error');
    progress = { ...progress, [m.job_id]: 0 };
    run(m, 'upload', () => api.uploadTrackAudio(m.deezer_id, file, (p) => (progress = { ...progress, [m.job_id]: p })), 'added');
  }

  const COOKIE_ERRORS = { json: 'missing.cookiesJson', format: 'missing.cookiesFormat', no_youtube: 'missing.cookiesNoYouTube' };

  // Сервер сохраняет куки и сразу спрашивает YouTube: пускает ли сервер
  // (check) и отдаёт ли с ними песню, которой нужен вход (signin); requeued
  // — сколько песен снова в очереди (они уходят со страницы). Загружать
  // можно и заранее, пока YouTube пускает.
  async function uploadCookies(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    cookiesBusy = 'upload';
    try {
      const res = await api.uploadYouTubeCookies(file);
      cookies = res.cookies;
      const said = cookieUploadMessage(res);
      toast($t(said.key, said.params), said.kind, 6000);
      if (res.requeued > 0) load();
    } catch (err) {
      const key = COOKIE_ERRORS[err.data?.reason] || (err.status === 413 && 'missing.cookiesTooBig');
      toast(key ? $t(key) : err.message, 'error');
    } finally {
      cookiesBusy = '';
    }
  }

  async function removeCookies() {
    cookiesBusy = 'remove';
    try {
      await api.deleteYouTubeCookies();
      cookies = null;
      toast($t('missing.cookiesRemoved'));
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      cookiesBusy = '';
    }
  }

  const KINDS = {
    blocked: 'missing.blocked',
    signin: 'missing.signin',
    not_found: 'missing.notFound',
    download: 'missing.download',
    audio: 'missing.audio',
    other: 'missing.other',
  };
  const when = (iso) => new Date(iso).toLocaleString($lang, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  const day = (iso) => new Date(iso).toLocaleDateString($lang, { day: 'numeric', month: 'short', year: 'numeric' });

  $: cookiesStatus = cookies
    ? [
        $t('missing.cookiesUploaded', { date: when(cookies.updated_at), n: cookies.count }),
        cookies.signed_in && cookies.expires && $t('missing.cookiesExpire', { date: day(cookies.expires) }),
      ]
        .filter(Boolean)
        .join(' · ')
    : $t('missing.cookiesNone');
</script>

<div class="page-pad missing">
  <h1>{$t('missing.title')}</h1>
  <p class="muted intro">{$t('missing.intro')}</p>

  {#if tracks}
    <section class="youtube" class:alert={blocked} role={blocked ? 'status' : null}>
      {#if blocked}
        <h2><ShieldAlert size={20} /> {$t('missing.blockedTitle')}</h2>
        <p>{$t('missing.blockedText')}</p>
        {#if proxied}
          <p>{$t('missing.blockedProxy')}</p>
        {:else}
          <ol>
            <li>{$t('missing.blockedStep1')}<code>YTDLP_PROXY=socks5://host.docker.internal:40000</code></li>
            <li>{$t('missing.blockedStep2')}<code>docker compose up -d</code></li>
          </ol>
        {/if}
        <p class="or">
          {$t('missing.cookiesOr')}
          {$t('missing.cookiesSteps')}
          <a href={COOKIES_HELP} target="_blank" rel="noopener">{$t('missing.cookiesMore')}</a>
        </p>
      {/if}
      <div class="cookies">
        <div class="label">
          <span>{$t('missing.cookies')}</span>
          <small>{cookiesStatus}</small>
          {#if cookies && !cookies.signed_in}<small class="warn">{$t('missing.cookiesSignedOut')}</small>{/if}
        </div>
        <div class="buttons">
          <label class="pill outline file" class:disabled={!!cookiesBusy}>
            {#if cookiesBusy === 'upload'}
              <LoaderCircle class="spin" size={16} /> {$t('missing.cookiesChecking')}
            {:else}
              <Cookie size={16} /> {$t(cookies ? 'missing.cookiesReplace' : 'missing.cookiesUpload')}
            {/if}
            <input type="file" accept=".txt,text/plain" hidden disabled={!!cookiesBusy} on:change={uploadCookies} />
          </label>
          {#if cookies}
            <button class="pill outline" disabled={!!cookiesBusy} on:click={removeCookies}>{$t('missing.cookiesRemove')}</button>
          {/if}
        </div>
      </div>
      {#if !blocked && !cookies}
        <details class="how">
          <summary>{$t('missing.cookiesWhy')}</summary>
          <p>
            {$t('missing.cookiesWhyText')}
            {$t('missing.cookiesSteps')}
            <a href={COOKIES_HELP} target="_blank" rel="noopener">{$t('missing.cookiesMore')}</a>
          </p>
        </details>
      {/if}
    </section>
  {/if}

  {#if error}
    <p class="error-text">{error}</p>
  {:else if tracks && !tracks.length}
    <div class="empty">
      <h2>{$t('missing.empty')}</h2>
    </div>
  {:else if tracks}
    <ul class="list">
      {#each tracks as m (m.job_id)}
        <li class="row" class:resolved={done[m.job_id]}>
          <div class="art">
            {#if m.cover_url}<img src={sized(m.cover_url, 200)} alt="" loading="lazy" />{:else}<Disc3 size={24} />{/if}
          </div>
          <div class="info">
            <div class="title">{m.title}</div>
            <div class="sub">
              {m.artist}
              {#if m.album}·
                {#if m.album_id}<a href={paths.album(m.album_id)} use:link>{m.album}</a>{:else}{m.album}{/if}
              {/if}
              · {formatTime(m.duration_ms / 1000)}
            </div>
            <div class="meta">
              <span class="chip">{$t(KINDS[m.kind] || 'missing.other')}</span>
              <span>{$t('missing.attempts', { n: m.attempts })}</span>
              {#if m.requested_by}<span>{$t('missing.requestedBy', { name: m.requested_by })}</span>{/if}
              <span>{when(m.updated_at)}</span>
            </div>
            {#if m.kind === 'signin'}<p class="hint">{$t(signInHint(m.error, !!cookies))}</p>{/if}
            <details class="why"><summary>{$t('missing.why')}</summary><code>{m.error}</code></details>
          </div>

          {#if done[m.job_id]}
            <div class="status">{done[m.job_id] === 'added' ? $t('missing.added') : $t('missing.queued')}</div>
          {:else}
            <div class="actions">
              <form class="video" on:submit|preventDefault={() => useVideo(m)}>
                <input
                  type="url"
                  inputmode="url"
                  placeholder={$t('missing.videoPlaceholder')}
                  aria-label={$t('missing.video')}
                  bind:value={links[m.job_id]}
                />
                <button class="pill" type="submit" disabled={!links[m.job_id]?.trim() || !!busy[m.job_id]}>
                  {#if busy[m.job_id] === 'video'}<LoaderCircle class="spin" size={16} />{:else}<Link size={16} />{/if}
                  {$t('missing.useVideo')}
                </button>
              </form>
              <div class="buttons">
                <label class="pill outline file" class:disabled={!!busy[m.job_id]}>
                  {#if busy[m.job_id] === 'upload'}
                    <LoaderCircle class="spin" size={16} />
                    {progress[m.job_id] < 1 ? $t('edit.uploading', { pct: Math.floor((progress[m.job_id] || 0) * 100) }) : $t('edit.preparing')}
                  {:else}
                    <FileMusic size={16} /> {$t('missing.upload')}
                  {/if}
                  <input
                    type="file"
                    accept="audio/*,.flac,.m4a,.mp3,.wav,.ogg,.opus,.aac,.alac,.aiff"
                    hidden
                    disabled={!!busy[m.job_id]}
                    on:change={(e) => upload(m, e)}
                  />
                </label>
                <button class="pill outline" disabled={!!busy[m.job_id]} on:click={() => retry(m)}>
                  {#if busy[m.job_id] === 'retry'}<LoaderCircle class="spin" size={16} />{:else}<RotateCw size={16} />{/if}
                  {$t('missing.retry')}
                </button>
              </div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {:else}
    <div class="loading"><LoaderCircle class="spin" size={24} /></div>
  {/if}
</div>

<style>
  .missing {
    padding-top: 24px;
    padding-bottom: 24px;
  }
  h1 {
    font-size: 32px;
  }
  .intro {
    margin: 8px 0 24px;
    max-width: 680px;
    font-size: 14px;
  }
  .youtube {
    max-width: 680px;
    margin: 0 0 24px;
    padding: 12px 20px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
    font-size: 14px;
  }
  .youtube.alert {
    padding-top: 16px;
    border-left: 3px solid var(--danger);
  }
  .youtube h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    font-size: 18px;
  }
  .youtube h2 :global(svg) {
    flex-shrink: 0;
    color: var(--danger);
  }
  .youtube p {
    margin: 0 0 8px;
    color: var(--text-dim);
  }
  .youtube .or {
    margin-top: 14px;
  }
  .youtube a {
    color: var(--text);
    text-decoration: underline;
  }
  .youtube ol {
    margin: 0;
    padding-left: 20px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    color: var(--text-dim);
  }
  .cookies {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 16px;
  }
  .alert .cookies {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }
  .cookies .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .cookies .label span {
    font-weight: 600;
  }
  .cookies .label small {
    font-size: 13px;
    color: var(--text-dim);
  }
  .cookies .label .warn {
    color: var(--danger);
  }
  .how {
    margin-top: 6px;
    font-size: 13px;
    color: var(--text-dim);
  }
  .how summary {
    width: fit-content;
    cursor: pointer;
  }
  .youtube .how p {
    margin: 6px 0 4px;
    font-size: 13px;
  }
  .youtube code {
    display: block;
    margin-top: 6px;
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 13px;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .row {
    display: grid;
    grid-template-columns: 56px minmax(0, 1fr) minmax(0, 420px);
    gap: 16px;
    align-items: center;
    padding: 12px;
    border-radius: 8px;
    background: var(--bg-elevated);
  }
  .row.resolved {
    opacity: 0.6;
  }
  .art {
    width: 56px;
    height: 56px;
    border-radius: 4px;
    overflow: hidden;
    background: var(--bg-press);
    color: var(--text-faint);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .art img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .title {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: 13px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .sub a:hover {
      text-decoration: underline;
    }
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    margin-top: 6px;
    font-size: 12px;
    color: var(--text-faint);
  }
  .chip {
    padding: 1px 8px;
    border-radius: 999px;
    background: var(--bg-hover);
    color: var(--text-dim);
  }
  .hint {
    margin: 6px 0 0;
    max-width: 560px;
    font-size: 13px;
    color: var(--text-dim);
  }
  .why {
    margin-top: 4px;
    font-size: 12px;
    color: var(--text-faint);
  }
  .why summary {
    cursor: pointer;
    width: fit-content;
  }
  .why code {
    display: block;
    margin-top: 4px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .actions {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .video {
    display: flex;
    gap: 8px;
  }
  .video input {
    flex: 1;
    min-width: 0;
    height: 32px;
    padding: 0 12px;
    border-radius: 999px;
    border: 1px solid transparent;
    background: var(--bg-input);
    font-size: 14px;
  }
  .video input:focus {
    outline: none;
    border-color: var(--text-faint);
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .file {
    cursor: pointer;
  }
  .file.disabled {
    opacity: 0.4;
    cursor: default;
  }
  .status {
    font-size: 14px;
    color: var(--ok);
  }
  .empty {
    padding: 32px 0;
  }
  .loading {
    padding: 48px 0;
    color: var(--text-dim);
  }
  @media (max-width: 900px) {
    .row {
      grid-template-columns: 48px minmax(0, 1fr);
    }
    .art {
      width: 48px;
      height: 48px;
    }
    .actions,
    .status {
      grid-column: 1 / -1;
    }
  }
  @media (max-width: 768px) {
    h1 {
      font-size: 26px;
    }
  }
</style>
