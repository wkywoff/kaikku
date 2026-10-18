<script>
  // «Не нашлось» (админ, /admin/missing): треки, которые не удалось скачать
  // — чаще всего на YouTube не нашлось подходящего видео. По каждому: дать
  // ссылку на видео (сервер скачает с него, без поиска), загрузить свой
  // файл или повторить поиск. Воркер и сам повторяет такие раз в сутки, до
  // трёх попыток (internal/ingest/housekeep.go).
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { paths, link } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { toast } from '../stores/toast.js';
  import { sized } from '../imageproxy.js';
  import { formatTime } from '../stores/player.js';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import FileMusic from '@lucide/svelte/icons/file-music';
  import Link from '@lucide/svelte/icons/link';
  import Disc3 from '@lucide/svelte/icons/disc-3';

  pageTint().none();

  const MAX_AUDIO = 200 * 1024 * 1024; // как на сервере (handlers_admin.go)

  let tracks = null;
  let error = '';
  let links = {}; // job_id → введённая ссылка
  let busy = {}; // job_id → 'video' | 'upload' | 'retry'
  let progress = {}; // job_id → доля загрузки файла
  let done = {}; // job_id → 'queued' | 'added': уже не «не нашлось»

  async function load() {
    try {
      tracks = (await api.missing()).tracks;
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

  const KINDS = { not_found: 'missing.notFound', download: 'missing.download', audio: 'missing.audio', other: 'missing.other' };
  const when = (iso) => new Date(iso).toLocaleString($lang, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
</script>

<div class="page-pad missing">
  <h1>{$t('missing.title')}</h1>
  <p class="muted intro">{$t('missing.intro')}</p>

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
