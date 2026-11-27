<script>
  import { onMount, createEventDispatcher } from 'svelte';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { toSquareJpeg } from '../image.js';
  import Dialog from './Dialog.svelte';
  import ImageUp from '@lucide/svelte/icons/image-up';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import ArtistPicker from './upload/ArtistPicker.svelte';
  import { createNewArtists } from '../uploads.js';
  import Download from '@lucide/svelte/icons/download';
  import FileMusic from '@lucide/svelte/icons/file-music';
  import MicVocal from '@lucide/svelte/icons/mic-vocal';
  import { sized } from '../imageproxy.js';
  import { toast } from '../stores/toast.js';
  import { forgetLyrics } from './LyricsView.svelte';
  import { clearPageCaches } from '../lru.js';
  import { trackIdFrom, isShortDeezerLink, isLocalId } from '../links.js';
  import { AUDIO_ACCEPT, MAX_AUDIO_BYTES } from '../uploads.js';
  import { paths, link } from '../stores/nav.js';
  import Pencil from '@lucide/svelte/icons/pencil';

  export let kind;
  export let id = null;
  export let deezerId = null;
  export let albumId = null;
  const byDeezer = kind === 'track';
  const own = kind === 'track' && isLocalId(deezerId);

  const dispatch = createEventDispatcher();

  let item = null;
  let error = '';
  let busy = false;
  let name = '';
  let trackNo = '';
  let discNo = '';
  let explicit = false;
  let artists = [];
  let initial = { name: '', trackNo: null, discNo: null, explicit: false, artists: '' };
  let toolBusy = '';
  let newImage = null;
  let newImageUrl = null;
  let fileInput;
  let audioInput;
  let uploadPct = null;
  let swapLink = '';
  let ownArtists = false;
  let lyrics;
  let lyricsInitial = '';

  $: imageUrl = newImageUrl || (kind === 'track' ? null : sized(item?.image_url, 300));
  $: customImage = kind === 'artist' ? item?.picture_custom : item?.cover_custom;
  $: canReset = !own && (byDeezer || !!item?.deezer_id) && !isLocalId(item?.deezer_id) && (item?.edited || customImage);

  onMount(() => {
    load();
    return () => {
      if (newImageUrl) URL.revokeObjectURL(newImageUrl);
    };
  });

  async function load() {
    try {
      if (own) {
        item = await api.getOwnTrack(deezerId);
        api.trackLyricsText(deezerId).then(
          (r) => (lyrics = lyricsInitial = r.text || ''),
          () => (lyrics = lyricsInitial = ''),
        );
      } else if (byDeezer) item = await api.getDeezerTrackEdit(deezerId);
      else {
        if (!id) id = (await api.claim(kind, deezerId)).id;
        item = await api.getArtist(id);
      }
      fill(item);
    } catch (e) {
      error = e.message;
    }
  }

  function fill(it) {
    name = kind === 'artist' ? it.name : it.title;
    trackNo = it.track_no ?? '';
    discNo = it.disc_no ?? '';
    explicit = !!it.explicit;
    artists = (it.artists || (own && it.release_artists) || []).map((a) => ({ id: String(a.id), name: a.name, picture_url: a.picture_url || null }));
    ownArtists = own && !!it.artists;
    initial = { name, trackNo: numberOrNull(trackNo), discNo: numberOrNull(discNo), explicit, artists: ids(artists) };
  }

  const ids = (list) => list.map((a) => a.id).join(',');
  const numberOrNull = (value) => (value === '' || value == null ? null : Number(value));
  $: editCredits = kind !== 'artist' && (own ? ownArtists : !!item?.artists?.length);

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

  async function uploadAudio(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    error = '';
    if (file.size > MAX_AUDIO_BYTES) {
      error = $t('edit.audioTooBig');
      return;
    }
    toolBusy = 'upload';
    uploadPct = 0;
    try {
      if (own) await api.replaceTrackAudio(deezerId, file, { onProgress: (p) => (uploadPct = p) });
      else await api.uploadTrackAudio(deezerId, file, (p) => (uploadPct = p));
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
      if (editCredits) [artists] = await createNewArtists([artists], api.createArtist);
      if (kind === 'artist') {
        await api.updateArtist(id, { name });
        if (newImage) await api.uploadArtistPicture(id, newImage);
      } else if (own) {
        await api.saveOwnTrack(deezerId, { title: name, explicit, artists: ownArtists ? artists.map((a) => a.id) : null });
        if (lyrics !== undefined && lyrics !== lyricsInitial) await api.saveTrackLyrics(deezerId, lyrics);
      } else {
        const numbers = { track_no: numberOrNull(trackNo), disc_no: numberOrNull(discNo) };
        const renamed = name.trim() !== initial.name.trim();
        const renumbered = numbers.track_no !== initial.trackNo || numbers.disc_no !== initial.discNo;
        const fields = renamed || renumbered ? { title: name, ...numbers } : {};
        if (editCredits && ids(artists) !== initial.artists) fields.artists = artists.map((a) => a.id);
        if (explicit !== initial.explicit) fields.explicit = explicit;
        await api.updateDeezerTrack(deezerId, fields);
      }
      clearPageCaches();
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
      else await api.resetDeezerTrack(deezerId);
      clearPageCaches();
      dispatch('saved');
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  const titles = { artist: 'edit.artistTitle', track: 'edit.trackTitle' };
</script>

<Dialog title={$t(titles[kind])} locked={busy} onSubmit={save} on:close={() => dispatch('close')}>
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
          <input class="input" type="text" bind:value={name} maxlength="300" autofocus />
        </label>

        {#if own}
          <label class="check">
            <input type="checkbox" bind:checked={explicit} />
            <span>{$t('edit.explicit')}</span>
          </label>
        {:else if kind === 'track'}
          <div class="row2">
            <label>
              <span>{$t('edit.trackNo')}</span>
              <input class="input" type="number" min="1" max="999" bind:value={trackNo} />
            </label>
            <label>
              <span>{$t('edit.discNo')}</span>
              <input class="input" type="number" min="1" max="99" bind:value={discNo} />
            </label>
          </div>
          <label class="check">
            <input type="checkbox" bind:checked={explicit} />
            <span>{$t('edit.explicit')}</span>
          </label>
        {/if}
      </div>
    </div>

    {#if own}
      <div class="credits">
        <label class="check">
          <input type="checkbox" bind:checked={ownArtists} on:change={() => !ownArtists && (artists = (item.release_artists || []).map((a) => ({ id: String(a.id), name: a.name, picture_url: a.picture_url || null })))} />
          <span>{$t('upload.ownTrackArtists')}<small>{ownArtists ? $t('upload.ownTrackArtistsHint') : $t('upload.asRelease', { names: (item.release_artists || []).map((a) => a.name).join(', ') || '—' })}</small></span>
        </label>
      </div>
    {/if}

    {#if editCredits}
      <div class="credits">
        <span class="caption">{$t('edit.artists')}</span>
        <ArtistPicker bind:artists invalid={!artists.length} inputId="edit-artists" label={$t('edit.artists')} />
      </div>
    {/if}

    {#if kind === 'track' && albumId && !own}
      <div class="credits">
        <span class="caption">{$t('edit.swapTrack')}</span>
        <div class="add-artist">
          <input class="input" type="text" bind:value={swapLink} placeholder={$t('edit.trackLink')} on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), swapTrack())} />
          <button type="button" class="pill outline lg" disabled={!swapLink.trim() || !!toolBusy} on:click={swapTrack}>
            {#if toolBusy === 'swap'}<LoaderCircle class="spin" size={16} />{/if}
            {$t('edit.replace')}
          </button>
        </div>
        <small class="hint">{$t('edit.swapHint')}</small>
      </div>
    {/if}

    {#if own}
      <label class="credits lyrics">
        <span class="caption">{$t('upload.lyrics')}</span>
        {#if lyrics === undefined}
          <LoaderCircle class="spin" size={16} />
        {:else}
          <textarea class="input" rows="6" maxlength="60000" bind:value={lyrics}></textarea>
        {/if}
        <small class="hint">{$t('upload.lyricsHint')}</small>
      </label>
      <div class="tools">
        <button type="button" class="pill outline lg" disabled={!!toolBusy} on:click={() => audioInput.click()}>
          {#if toolBusy === 'upload'}<LoaderCircle class="spin" size={16} />{:else}<FileMusic size={16} />{/if}
          {#if uploadPct === null}{$t('upload.replaceAudio')}
          {:else if uploadPct < 1}{$t('edit.uploading', { pct: Math.floor(uploadPct * 100) })}
          {:else}{$t('edit.preparing')}{/if}
        </button>
        <input bind:this={audioInput} type="file" accept={AUDIO_ACCEPT} hidden on:change={uploadAudio} />
        <a class="pill outline lg" href={paths.releaseEdit(item.album_id)} use:link on:click={() => dispatch('close')}><Pencil size={16} /> {$t('upload.editRelease')}</a>
      </div>
    {:else if kind === 'track'}
      <div class="credits">
        <span class="caption">{$t('edit.video')}</span>
        <div class="add-artist">
          <input class="input" type="url" inputmode="url" bind:value={videoLink} placeholder={$t('missing.videoPlaceholder')} on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), useVideo())} />
          <button type="button" class="pill outline lg" disabled={!videoLink.trim() || !!toolBusy} on:click={useVideo}>
            {#if toolBusy === 'video'}<LoaderCircle class="spin" size={16} />{/if}
            {$t('missing.useVideo')}
          </button>
        </div>
        <small class="hint">{$t('edit.videoHint')}</small>
      </div>
      <div class="tools">
        {#if item.library_id}
          <button type="button" class="pill outline lg" disabled={!!toolBusy} on:click={redownload}>
            {#if toolBusy === 'audio'}<LoaderCircle class="spin" size={16} />{:else}<Download size={16} />{/if}
            {$t('edit.redownload')}
          </button>
        {/if}
        {#if item.library_id || item.missing}
          <button type="button" class="pill outline lg" disabled={!!toolBusy} on:click={() => audioInput.click()}>
            {#if toolBusy === 'upload'}<LoaderCircle class="spin" size={16} />{:else}<FileMusic size={16} />{/if}
            {#if uploadPct === null}{$t('edit.uploadAudio')}
            {:else if uploadPct < 1}{$t('edit.uploading', { pct: Math.floor(uploadPct * 100) })}
            {:else}{$t('edit.preparing')}{/if}
          </button>
          <input bind:this={audioInput} type="file" accept="audio/*,.flac,.m4a,.mp3,.wav,.ogg,.opus,.aac,.alac,.aiff" hidden on:change={uploadAudio} />
        {/if}
        <button type="button" class="pill outline lg" disabled={!!toolBusy} on:click={refreshLyrics}>
          {#if toolBusy === 'lyrics'}<LoaderCircle class="spin" size={16} />{:else}<MicVocal size={16} />{/if}
          {$t('edit.refreshLyrics')}
        </button>
      </div>
    {/if}
  {/if}

  {#if error}<p class="error-text">{error}</p>{/if}

  <svelte:fragment slot="footer">
    {#if canReset}
      <button type="button" class="text-btn" on:click={reset} disabled={busy}><RotateCcw size={16} /> {$t('edit.reset')}</button>
    {/if}
    <button type="submit" class="pill primary lg" disabled={busy || !item || (editCredits && !artists.length)}>
      {#if busy}<LoaderCircle class="spin" size={16} />{/if}
      {$t('edit.save')}
    </button>
  </svelte:fragment>
</Dialog>

<style>
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
    border-radius: var(--radius-sm);
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
    background: rgba(0, 0, 0, 0.6);
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
  .lyrics {
    font-weight: 400;
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
  }
  .caption {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
    margin-bottom: 2px;
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
  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
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
