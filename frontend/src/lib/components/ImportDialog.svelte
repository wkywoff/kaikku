<script>
  // Импорт плейлиста: ссылка Spotify (первые 100 треков — больше
  // встраиваемый плеер Spotify не отдаёт), CSV-выгрузка (Exportify и т.п.)
  // или список «Исполнитель — Трек». Треки ищутся в каталоге пачками
  // (internal/api/import.go), затем треки одного исполнителя сводятся на
  // один релиз (import_albums.go), найденное становится новым плейлистом.
  import { modal } from '../modal.js';
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { refreshPlaylists } from '../stores/collection.js';
  import { parseTrackList, spotifyLink } from '../importList.js';
  import X from '@lucide/svelte/icons/x';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import FileUp from '@lucide/svelte/icons/file-up';

  const dispatch = createEventDispatcher();
  const BATCH = 20; // треков на запрос (сервер: maxImportMatch)
  // Треков на одно добавление в плейлист: сервер берёт каждый у Deezer
  // (40 запросов за 5 с на всех), 50 укладываются в несколько секунд.
  const ADD_CHUNK = 50;

  let step = 'input'; // input | matching | settling | done
  let text = '';
  let error = '';
  let busy = false;
  let source = null; // { title, spotify, truncated }
  let tracks = []; // что ищем
  let matches = []; // найденное (Deezer-трек или null) по порядку tracks
  let done = 0;
  let title = '';
  let cancelled = false;
  let fileInput;
  let textarea;

  $: found = matches.filter(Boolean);
  $: missing = tracks.filter((_, i) => matches[i] === null);

  onMount(() => {
    textarea?.focus();
    const onKey = (e) => e.key === 'Escape' && !busy && close();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
  onDestroy(() => (cancelled = true));

  function close() {
    cancelled = true;
    dispatch('close');
  }

  async function readFile(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (file) text = await file.text();
  }

  async function start() {
    error = '';
    busy = true;
    created = null;
    added = 0;
    try {
      const link = spotifyLink(text);
      if (link) {
        const list = await api.importSpotify(link);
        source = { title: list.title, spotify: true, truncated: list.truncated };
        tracks = list.tracks;
      } else {
        source = { title: '', spotify: false, truncated: false };
        tracks = parseTrackList(text);
      }
      if (!tracks.length) {
        error = $t('import.nothing');
        return;
      }
      title = source.title || $t('import.defaultTitle');
      await match();
    } catch (e) {
      error = e.message;
      step = 'input';
    } finally {
      busy = false;
    }
  }

  async function match() {
    step = 'matching';
    matches = [];
    done = 0;
    for (let i = 0; i < tracks.length && !cancelled; i += BATCH) {
      const batch = tracks.slice(i, i + BATCH);
      matches = [...matches, ...(await matchBatch(batch))];
      done = Math.min(tracks.length, i + BATCH);
    }
    if (cancelled) return;
    if (matches.filter(Boolean).length > 1) {
      step = 'settling';
      matches = await settleAlbums(matches);
    }
    if (!cancelled) step = 'done';
  }

  // Песни одного альбома — с одного релиза, а не вперемешку с делюкса,
  // сингла и сборника. Не вышло — оставляем как нашлось.
  async function settleAlbums(list) {
    try {
      const settled = (await api.importAlbums(list)).matches;
      return settled?.length === list.length ? settled : list;
    } catch {
      return list;
    }
  }

  // Deezer бывает занят (общий лимит запросов) — пачку пробуем ещё пару раз.
  async function matchBatch(batch) {
    for (let attempt = 0; attempt < 3 && !cancelled; attempt++) {
      try {
        return (await api.importMatch(batch)).matches;
      } catch {
        await new Promise((r) => setTimeout(r, 3000 * (attempt + 1)));
      }
    }
    return batch.map(() => null);
  }

  // Плейлист уже заведён и сколько в него добавлено: если добавление
  // оборвалось, повторное «Создать» продолжает с того же места, а не
  // заводит второй такой же плейлист.
  let created = null;
  let added = 0;

  async function create() {
    busy = true;
    error = '';
    try {
      const ids = [...new Set(found.map((m) => m.id))];
      created ||= await api.createPlaylist({
        title: title.trim() || $t('import.defaultTitle'),
        description: source.spotify ? $t('import.fromSpotify') : '',
      });
      while (added < ids.length) {
        const chunk = ids.slice(added, added + ADD_CHUNK);
        await api.addToPlaylist(created.id, chunk, { download: false });
        added += chunk.length;
      }
      await refreshPlaylists();
      dispatch('created', created);
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  const label = (tr) => [tr.artists?.join(', '), tr.title].filter(Boolean).join(' — ');
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={() => !busy && close()} transition:fade={{ duration: 120 }} on:click|self={() => !busy && close()} role="dialog" tabindex="-1" aria-modal="true" aria-label={$t('import.title')}>
  <div class="dialog" transition:scale={{ start: 0.96, duration: 150 }}>
    <div class="head">
      <h2>{$t('import.title')}</h2>
      <button type="button" class="icon-btn" on:click={close} aria-label={$t('album.close')} disabled={busy && step !== 'matching' && step !== 'settling'}><X size={20} /></button>
    </div>

    {#if step === 'input'}
      <p class="hint">{$t('import.hint')}</p>
      <textarea bind:this={textarea} bind:value={text} rows="7" placeholder={$t('import.placeholder')} disabled={busy}></textarea>
      <div class="row">
        <button type="button" class="link" on:click={() => fileInput.click()} disabled={busy}><FileUp size={16} /> {$t('import.csv')}</button>
        <input bind:this={fileInput} type="file" accept=".csv,.txt,text/csv,text/plain" hidden on:change={readFile} />
        <div class="spacer"></div>
        <button type="button" class="primary" on:click={start} disabled={busy || !text.trim()}>
          {#if busy}<LoaderCircle class="spin" size={16} />{/if}
          {$t('import.find')}
        </button>
      </div>
    {:else if step === 'matching'}
      <p>{$t('import.matching', { done, total: tracks.length })}</p>
      <div class="bar"><div style="width: {(done / tracks.length) * 100}%"></div></div>
    {:else if step === 'settling'}
      <p>{$t('import.settling')}</p>
      <div class="bar"><div style="width: 100%"></div></div>
    {:else}
      <p class="summary">{$t('import.found', { found: found.length, total: tracks.length })}</p>
      {#if source.truncated}<p class="hint warn">{$t('import.truncated')}</p>{/if}
      <label>
        <span>{$t('playlist.name')}</span>
        <input type="text" bind:value={title} maxlength="300" />
      </label>
      {#if missing.length}
        <details>
          <summary>{$t('import.missing', { n: missing.length })}</summary>
          <ul>
            {#each missing.slice(0, 100) as tr}<li>{label(tr)}</li>{/each}
          </ul>
        </details>
      {/if}
      <div class="row">
        <div class="spacer"></div>
        <button type="button" class="primary" on:click={create} disabled={busy || !found.length}>
          {#if busy}<LoaderCircle class="spin" size={16} />{/if}
          {$t('import.create')}
        </button>
      </div>
    {/if}

    {#if error}<p class="error-text">{error}</p>{/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px 16px max(16px, env(safe-area-inset-bottom));
    background: rgba(0, 0, 0, 0.7);
  }
  .dialog {
    width: min(560px, 100%);
    max-height: 100%; /* по видимой высоте окна (в отступах подложки), а не 100vh — во вкладке браузера адресная строка её отъедает */
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 24px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h2 {
    font-size: 22px;
  }
  .hint {
    color: var(--text-dim);
    font-size: 14px;
  }
  .warn {
    color: #f0c674;
  }
  textarea,
  input[type='text'] {
    padding: 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font-size: 14px;
    font-family: inherit;
    resize: vertical;
  }
  textarea:focus,
  input[type='text']:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .spacer {
    flex: 1;
  }
  .link {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .link:hover:not(:disabled) {
      color: var(--text);
    }
  }
  .primary {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 44px;
    padding: 0 28px;
    border-radius: 999px;
    background: #fff;
    color: #000;
    font-weight: 700;
  }
  .primary:disabled {
    opacity: 0.6;
  }
  .bar {
    height: 6px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.15);
    overflow: hidden;
  }
  .bar div {
    height: 100%;
    background: var(--accent);
    transition: width 0.3s;
  }
  .summary {
    font-size: 18px;
    font-weight: 700;
  }
  details {
    font-size: 14px;
    color: var(--text-dim);
  }
  summary {
    cursor: pointer;
  }
  ul {
    margin: 8px 0 0;
    padding-left: 18px;
    max-height: 180px;
    overflow-y: auto;
  }
</style>
