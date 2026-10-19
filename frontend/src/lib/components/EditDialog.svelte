<script>
  // Правка метаданных админом: артист (имя, фото), альбом (название, дата
  // релиза, тип, обложка, исполнители, скрыть), трек (название, номер, диск,
  // исполнители, метка E). Артист и альбом правятся по записи в медиатеке
  // (id — наш); если её ещё нет — по deezerId сервер её заводит. Трек
  // правится по Deezer ID, скачивать его не нужно.
  // Исполнители задаются ссылками на артистов (Deezer залил релиз дубликату
  // артиста — ссылка на настоящего переносит релиз к нему).
  // Альбом можно привязать версией к основному релизу (делюкс, clean):
  // пропадёт из дискографии, будет в «Других версиях» основного.
  // У альбома — треклист: порядок и трек с другого релиза по ссылке
  // (TracklistEditor); у трека, открытого со страницы альбома, — та же замена.
  // У трека ещё «перекачать аудио», «загрузить свой файл» и «найти текст заново».
  // «Сбросить» возвращает данные и картинку из Deezer.
  // Своего артиста (загрузка, k…) здесь правит тот, кто его завёл: имя и
  // фото, без сброса — сбрасывать не к чему.
  import { modal } from '../modal.js';
  import { onMount, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { toSquareJpeg } from '../image.js';
  import X from '@lucide/svelte/icons/x';
  import ImageUp from '@lucide/svelte/icons/image-up';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import ArtistPicker from './upload/ArtistPicker.svelte';
  import Download from '@lucide/svelte/icons/download';
  import FileMusic from '@lucide/svelte/icons/file-music';
  import MicVocal from '@lucide/svelte/icons/mic-vocal';
  import { sized } from '../imageproxy.js';
  import { toast } from '../stores/toast.js';
  import { forgetLyrics } from './LyricsView.svelte';
  import { clearPageCaches } from '../lru.js';
  import TracklistEditor from './TracklistEditor.svelte';
  import { trackIdFrom, albumIdFrom, isShortDeezerLink, isLocalId } from '../links.js';

  export let kind; // 'artist' | 'album' | 'track'
  export let id = null; // артист/альбом в медиатеке
  export let deezerId = null; // артист/альбом, если id ещё нет; трек — всегда
  export let albumId = null; // трек: Deezer ID альбома, со страницы которого открыли
  const byDeezer = kind === 'track';

  const dispatch = createEventDispatcher();

  let item = null;
  let error = '';
  let busy = false;
  let name = '';
  let releasedAt = '';
  let recordType = 'album';
  let trackNo = '';
  let discNo = '';
  let explicit = false;
  let hidden = false;
  let artists = []; // [{ id, name, picture_url }] — основной первый
  let initial = { explicit: false, hidden: false, artists: '' };
  let toolBusy = '';
  let newImage = null; // Blob
  let newImageUrl = null;
  let fileInput;
  let audioInput;
  let uploadPct = null; // 0..1 — отправка; 1 — сервер обрабатывает
  let tracklist = null; // альбом: [{ id, title, artist, other }]
  let initialTracklist = '';
  let swapLink = '';
  let versionOf = null; // альбом: { id, title, cover_url, release_date } основного релиза или null
  let initialVersionOf = '';
  let versionLink = '';
  let versionError = '';
  let versionBusy = false;

  // Картинка, какую видят все: своя или с Deezer (своих копий Deezer не храним).
  $: imageUrl = newImageUrl || (kind === 'track' ? null : sized(item?.image_url, 300));
  $: customImage = kind === 'artist' ? item?.picture_custom : item?.cover_custom;
  $: canReset = (byDeezer || !!item?.deezer_id) && !isLocalId(item?.deezer_id) && (item?.edited || customImage);

  // onMount синхронный: функцию очистки из async-колбэка Svelte не вызывает,
  // и обработчик Esc оставался бы висеть после закрытия окна.
  onMount(() => {
    load();
    const onKey = (e) => e.key === 'Escape' && !busy && dispatch('close');
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      if (newImageUrl) URL.revokeObjectURL(newImageUrl);
    };
  });

  async function load() {
    try {
      if (byDeezer) item = await api.getDeezerTrackEdit(deezerId);
      else {
        if (!id) id = (await api.claim(kind, deezerId)).id;
        item = kind === 'artist' ? await api.getArtist(id) : await api.getAlbum(id);
      }
      fill(item);
      if (kind === 'album') {
        loadTracklist();
        initialVersionOf = item.version_of || '';
        if (initialVersionOf) loadVersionOf(initialVersionOf);
      }
    } catch (e) {
      error = e.message;
    }
  }

  async function loadVersionOf(dz) {
    try {
      const { album } = await api.discoverAlbum(dz);
      versionOf = album;
    } catch {
      versionOf = { id: dz, title: dz };
    }
  }

  async function linkVersion() {
    versionError = '';
    if (isShortDeezerLink(versionLink)) {
      versionError = $t('edit.shortLink');
      return;
    }
    const dz = albumIdFrom(versionLink);
    if (!dz) {
      versionError = $t('edit.badAlbumLink');
      return;
    }
    versionBusy = true;
    try {
      const { album } = await api.discoverAlbum(dz);
      versionOf = album;
      versionLink = '';
    } catch (e) {
      versionError = e.message;
    } finally {
      versionBusy = false;
    }
  }

  async function loadTracklist() {
    const dz = item.deezer_id;
    if (!dz) return;
    try {
      const { tracks } = await api.discoverAlbum(dz);
      tracklist = tracks.map((tr) => ({ id: String(tr.id), title: tr.title, artist: tr.artist, other: String(tr.album_id) !== String(dz) }));
      initialTracklist = tracklist.map((r) => r.id).join(',');
    } catch {
      // Deezer недоступен — без треклиста, остальное править можно
    }
  }

  function fill(it) {
    name = kind === 'artist' ? it.name : it.title;
    releasedAt = it.released_at || '';
    recordType = it.record_type || 'album';
    trackNo = it.track_no ?? '';
    discNo = it.disc_no ?? '';
    explicit = !!it.explicit;
    hidden = !!it.hidden;
    artists = (it.artists || []).map((a) => ({ id: String(a.id), name: a.name, picture_url: a.picture_url || null }));
    initial = { explicit, hidden, artists: ids(artists) };
  }

  const ids = (list) => list.map((a) => a.id).join(',');
  // Исполнители известны, только если Deezer ответил: иначе их не трогаем.
  $: editCredits = kind !== 'artist' && !!item?.artists?.length;

  // Нашлось не то (или не нашлось вовсе) — скачать с указанного видео.
  let videoLink = '';
  async function useVideo() {
    toolBusy = 'video';
    error = '';
    try {
      await api.setTrackVideo(deezerId, videoLink);
      videoLink = '';
      toast($t('edit.videoQueued'));
    } catch (e) {
      error = e.message;
    } finally {
      toolBusy = '';
    }
  }

  async function redownload() {
    toolBusy = 'audio';
    error = '';
    try {
      await api.redownloadTrack(deezerId);
      toast($t('edit.redownloadQueued'));
    } catch (e) {
      error = e.message;
    } finally {
      toolBusy = '';
    }
  }

  const MAX_AUDIO = 200 * 1024 * 1024; // как на сервере (handlers_admin.go)

  async function uploadAudio(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    error = '';
    if (file.size > MAX_AUDIO) {
      error = $t('edit.audioTooBig');
      return;
    }
    toolBusy = 'upload';
    uploadPct = 0;
    try {
      await api.uploadTrackAudio(deezerId, file, (p) => (uploadPct = p));
      toast($t('edit.audioReplaced'));
    } catch (err) {
      error = err.status === 413 ? $t('edit.audioTooBig') : err.message;
    } finally {
      toolBusy = '';
      uploadPct = null;
    }
  }

  async function swapTrack() {
    error = '';
    if (isShortDeezerLink(swapLink)) {
      error = $t('edit.shortLink');
      return;
    }
    const id = trackIdFrom(swapLink);
    if (!id) {
      error = $t('edit.badTrackLink');
      return;
    }
    toolBusy = 'swap';
    try {
      await api.replaceAlbumTrack(albumId, deezerId, id);
      clearPageCaches();
      toast($t('edit.trackReplaced'));
      dispatch('saved');
    } catch (e) {
      error = e.message;
    } finally {
      toolBusy = '';
    }
  }

  async function refreshLyrics() {
    toolBusy = 'lyrics';
    error = '';
    try {
      const r = await api.refreshLyrics(deezerId);
      forgetLyrics(deezerId);
      toast($t(r.lyrics?.lines?.length ? 'edit.lyricsFound' : 'edit.lyricsNone'));
    } catch (e) {
      error = e.message;
    } finally {
      toolBusy = '';
    }
  }

  async function pickImage(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    error = '';
    try {
      newImage = await toSquareJpeg(file);
      if (newImageUrl) URL.revokeObjectURL(newImageUrl);
      newImageUrl = URL.createObjectURL(newImage);
    } catch {
      error = $t('edit.badImage');
    }
  }

  async function save() {
    error = '';
    if (!name.trim()) {
      error = $t('edit.nameRequired');
      return;
    }
    busy = true;
    try {
      if (kind === 'artist') {
        await api.updateArtist(id, { name });
        if (newImage) await api.uploadArtistPicture(id, newImage);
      } else if (kind === 'album') {
        // Исполнители и скрытие — только если их меняли.
        const fields = { title: name, released_at: releasedAt || '', record_type: recordType };
        if (editCredits && ids(artists) !== initial.artists) fields.artists = artists.map((a) => a.id);
        if (hidden !== initial.hidden) fields.hidden = hidden;
        if (tracklist && tracklist.map((r) => r.id).join(',') !== initialTracklist) fields.tracklist = tracklist.map((r) => r.id);
        if ((versionOf ? String(versionOf.id) : '') !== initialVersionOf) fields.version_of = versionOf ? String(versionOf.id) : '';
        await api.updateAlbum(id, fields);
        if (newImage) await api.uploadAlbumCover(id, newImage);
      } else {
        const fields = {
          title: name,
          track_no: trackNo === '' ? null : Number(trackNo),
          disc_no: discNo === '' ? null : Number(discNo),
        };
        if (editCredits && ids(artists) !== initial.artists) fields.artists = artists.map((a) => a.id);
        if (explicit !== initial.explicit) fields.explicit = explicit;
        await api.updateDeezerTrack(deezerId, fields);
      }
      clearPageCaches(); // релиз мог переехать к другому артисту — его страницы в кеше устарели
      dispatch('saved');
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  async function reset() {
    if (!confirm($t('edit.resetConfirm'))) return;
    busy = true;
    error = '';
    try {
      if (kind === 'artist') await api.resetArtist(id);
      else if (kind === 'album') await api.resetAlbum(id);
      else await api.resetDeezerTrack(deezerId);
      clearPageCaches(); // релиз мог переехать к другому артисту — его страницы в кеше устарели
      dispatch('saved');
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  const titles = { artist: 'edit.artistTitle', album: 'edit.albumTitle', track: 'edit.trackTitle' };
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={() => !busy && dispatch('close')} transition:fade={{ duration: 120 }} on:click|self={() => !busy && dispatch('close')} role="dialog" tabindex="-1" aria-modal="true" aria-label={$t(titles[kind])}>
  <form class="dialog" transition:scale={{ start: 0.96, duration: 150 }} on:submit|preventDefault={save}>
    <div class="head">
      <h2>{$t(titles[kind])}</h2>
      <button type="button" class="icon-btn" on:click={() => dispatch('close')} aria-label={$t('album.close')} disabled={busy}><X size={20} /></button>
    </div>

    {#if !item && !error}
      <div class="loading"><LoaderCircle class="spin" size={24} /></div>
    {:else if item}
      <div class="body" class:with-image={kind !== 'track'}>
        {#if kind !== 'track'}
          <div class="image-col">
            <button type="button" class="image" class:round={kind === 'artist'} on:click={() => fileInput.click()} title={$t('edit.chooseImage')}>
              {#if imageUrl}<img src={imageUrl} alt="" />{/if}
              <span class="image-overlay"><ImageUp size={28} /><span>{$t('edit.chooseImage')}</span></span>
            </button>
            <input bind:this={fileInput} type="file" accept="image/*" hidden on:change={pickImage} />
          </div>
        {/if}

        <div class="fields">
          <label>
            <span>{kind === 'artist' ? $t('edit.name') : $t('edit.title')}</span>
            <!-- svelte-ignore a11y_autofocus -->
            <input type="text" bind:value={name} maxlength="300" autofocus />
          </label>

          {#if kind === 'album'}
            <div class="row2">
              <label>
                <span>{$t('edit.releaseDate')}</span>
                <input type="date" bind:value={releasedAt} />
              </label>
              <label>
                <span>{$t('edit.type')}</span>
                <select bind:value={recordType}>
                  {#each ['album', 'ep', 'single', 'compile'] as type}
                    <option value={type}>{$t(`type.${type}`)}</option>
                  {/each}
                </select>
              </label>
            </div>
          {:else if kind === 'track'}
            <div class="row2">
              <label>
                <span>{$t('edit.trackNo')}</span>
                <input type="number" min="1" max="999" bind:value={trackNo} />
              </label>
              <label>
                <span>{$t('edit.discNo')}</span>
                <input type="number" min="1" max="99" bind:value={discNo} />
              </label>
            </div>
            <label class="check">
              <input type="checkbox" bind:checked={explicit} />
              <span>{$t('edit.explicit')}</span>
            </label>
          {/if}
          {#if kind === 'album'}
            <label class="check">
              <input type="checkbox" bind:checked={hidden} />
              <span>{$t('edit.hideRelease')}<small>{$t('edit.hideHint')}</small></span>
            </label>
          {/if}
        </div>
      </div>

      {#if editCredits}
        <!-- Как в загрузке: имя — подсказки (свои артисты, потом Deezer) и
             «Создать артиста»; ссылка на артиста — сразу он. -->
        <div class="credits">
          <span class="caption">{$t('edit.artists')}</span>
          <ArtistPicker bind:artists invalid={!artists.length} inputId="edit-artists" label={$t('edit.artists')} />
        </div>
      {/if}

      {#if kind === 'album' && item.deezer_id}
        <div class="credits">
          <span class="caption">{$t('edit.versionOf')}</span>
          {#if versionOf}
            <div class="credit">
              {#if versionOf.cover_url}<img class="sq" src={sized(versionOf.cover_url, 64)} alt="" />{/if}
              <span class="cname">{versionOf.title}</span>
              <button type="button" class="icon-btn small" title={$t('edit.unlinkVersion')} aria-label={$t('edit.unlinkVersion')} on:click={() => (versionOf = null)}><X size={16} /></button>
            </div>
          {:else}
            <div class="add-artist">
              <input type="text" bind:value={versionLink} placeholder={$t('edit.albumLink')} on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), linkVersion())} />
              <button type="button" class="add" disabled={!versionLink.trim() || versionBusy} on:click={linkVersion}>
                {#if versionBusy}<LoaderCircle class="spin" size={16} />{/if}
                {$t('edit.linkVersion')}
              </button>
            </div>
          {/if}
          {#if versionError}<p class="error-text">{versionError}</p>{/if}
          <small class="hint">{$t('edit.versionHint')}</small>
        </div>
      {/if}

      {#if kind === 'album' && tracklist}
        <TracklistEditor bind:list={tracklist} albumDeezerId={item.deezer_id} />
      {/if}

      {#if kind === 'track' && albumId}
        <div class="credits">
          <span class="caption">{$t('edit.swapTrack')}</span>
          <div class="add-artist">
            <input type="text" bind:value={swapLink} placeholder={$t('edit.trackLink')} on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), swapTrack())} />
            <button type="button" class="add" disabled={!swapLink.trim() || !!toolBusy} on:click={swapTrack}>
              {#if toolBusy === 'swap'}<LoaderCircle class="spin" size={16} />{/if}
              {$t('edit.replace')}
            </button>
          </div>
          <small class="hint">{$t('edit.swapHint')}</small>
        </div>
      {/if}

      {#if kind === 'track'}
        <div class="credits">
          <span class="caption">{$t('edit.video')}</span>
          <div class="add-artist">
            <input type="url" inputmode="url" bind:value={videoLink} placeholder={$t('missing.videoPlaceholder')} on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), useVideo())} />
            <button type="button" class="add" disabled={!videoLink.trim() || !!toolBusy} on:click={useVideo}>
              {#if toolBusy === 'video'}<LoaderCircle class="spin" size={16} />{/if}
              {$t('missing.useVideo')}
            </button>
          </div>
          <small class="hint">{$t('edit.videoHint')}</small>
        </div>
        <div class="tools">
          {#if item.library_id}
            <button type="button" class="tool" disabled={!!toolBusy} on:click={redownload}>
              {#if toolBusy === 'audio'}<LoaderCircle class="spin" size={16} />{:else}<Download size={16} />{/if}
              {$t('edit.redownload')}
            </button>
          {/if}
          <!-- Не скачался (missing) — свой файл и станет треком. -->
          {#if item.library_id || item.missing}
            <button type="button" class="tool" disabled={!!toolBusy} on:click={() => audioInput.click()}>
              {#if toolBusy === 'upload'}<LoaderCircle class="spin" size={16} />{:else}<FileMusic size={16} />{/if}
              {#if uploadPct === null}{$t('edit.uploadAudio')}
              {:else if uploadPct < 1}{$t('edit.uploading', { pct: Math.floor(uploadPct * 100) })}
              {:else}{$t('edit.preparing')}{/if}
            </button>
            <input bind:this={audioInput} type="file" accept="audio/*,.flac,.m4a,.mp3,.wav,.ogg,.opus,.aac,.alac,.aiff" hidden on:change={uploadAudio} />
          {/if}
          <button type="button" class="tool" disabled={!!toolBusy} on:click={refreshLyrics}>
            {#if toolBusy === 'lyrics'}<LoaderCircle class="spin" size={16} />{:else}<MicVocal size={16} />{/if}
            {$t('edit.refreshLyrics')}
          </button>
        </div>
      {/if}
    {/if}

    {#if error}<p class="error-text">{error}</p>{/if}

    <div class="footer">
      {#if canReset}
        <button type="button" class="reset" on:click={reset} disabled={busy}><RotateCcw size={16} /> {$t('edit.reset')}</button>
      {/if}
      <div class="spacer"></div>
      <button type="submit" class="save" disabled={busy || !item || (editCredits && !artists.length)}>
        {#if busy}<LoaderCircle class="spin" size={16} />{/if}
        {$t('edit.save')}
      </button>
    </div>
  </form>
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
    padding: 24px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }
  h2 {
    font-size: 22px;
  }
  .loading {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 40px 0;
  }
  .body {
    display: flex;
    gap: 20px;
  }
  .image {
    position: relative;
    width: 180px;
    height: 180px;
    border-radius: 4px;
    overflow: hidden;
    background: var(--bg-press);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  }
  .image.round {
    border-radius: 50%;
  }
  .image img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .image-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    background: rgba(0, 0, 0, 0.55);
    opacity: 0;
    transition: opacity 0.15s;
  }
  .image:focus-visible .image-overlay {
    opacity: 1;
  }
  @media (hover: hover) {
    .image:hover .image-overlay {
      opacity: 1;
    }
  }
  .fields {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
  }
  input,
  select {
    height: 40px;
    padding: 0 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
    color-scheme: dark;
  }
  input:focus,
  select:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .error-text {
    margin: 12px 0 0;
  }
  .check {
    flex-direction: row;
    align-items: flex-start;
    gap: 10px;
    font-size: 14px;
    color: var(--text);
    cursor: pointer;
  }
  .check input {
    width: 18px;
    height: 18px;
    margin: 1px 0 0;
    padding: 0;
    accent-color: var(--accent);
  }
  .check small {
    display: block;
    margin-top: 2px;
    font-size: 12px;
    font-weight: 400;
    color: var(--text-dim);
  }
  .credits {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 20px;
  }
  .caption {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
    margin-bottom: 2px;
  }
  .credit {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 40px;
    padding: 0 4px 0 6px;
    border-radius: 6px;
    background: #333;
  }
  .credit img {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
  }
  .credit img.sq {
    border-radius: 4px;
  }
  .cname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
  }
  .hint {
    font-size: 12px;
    color: var(--text-dim);
  }
  .add-artist {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }
  .add-artist input {
    flex: 1;
    min-width: 0;
  }
  .add,
  .tool {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 40px;
    padding: 0 16px;
    border-radius: 999px;
    border: 1px solid #6a6a6a;
    font-size: 14px;
    font-weight: 600;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .add:hover:not(:disabled),
    .tool:hover:not(:disabled) {
      border-color: #fff;
    }
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 20px;
  }
  .footer {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 24px;
  }
  .spacer {
    flex: 1;
  }
  .reset {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .reset:hover:not(:disabled) {
      color: var(--text);
    }
  }
  .save {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 44px;
    padding: 0 28px;
    border-radius: 999px;
    background: #fff;
    color: #000;
    font-weight: 700;
    transition: transform 0.1s;
  }
  @media (hover: hover) {
    .save:hover:not(:disabled) {
      transform: scale(1.04);
    }
  }
  @media (max-width: 560px) {
    .body {
      flex-direction: column;
      align-items: center;
    }
    .fields {
      width: 100%;
    }
  }
</style>
