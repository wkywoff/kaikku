<script>
  // Окно загрузки своего релиза (/upload/<id>, id — k…). Сверху — релиз:
  // обложка, название, исполнители, тип, дата; ниже — треки (TrackRows).
  // Файлы и обложка уходят на сервер сразу (файлы — по одному, очередью),
  // поля — кнопкой «Сохранить». «Опубликовать» проверяет, что всё нужное
  // есть (то же проверяет сервер), и открывает страницу релиза. Пустые поля
  // заполняются из тегов первого файла: название альбома, дата, исполнитель
  // (его остаётся выбрать в подсказках), обложка.
  // В треки можно добавить существующий трек по ссылке (Deezer или kaikku,
  // как замена трека в окне правки) — он встаёт в релиз как есть, его здесь
  // не правят; а релиз — привязать версией к другому (делюкс, clean): он
  // уйдёт из дискографии в «Другие версии» основного.
  import { onMount, onDestroy, tick } from 'svelte';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { navStore, paths, link, navigate, guardLeave } from '../stores/nav.js';
  import { toast } from '../stores/toast.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { setRouteTitle } from '../stores/title.js';
  import { pageTint } from '../stores/theme.js';
  import { clearPageCaches } from '../lru.js';
  import { toSquareJpeg } from '../image.js';
  import { sized } from '../imageproxy.js';
  import { trackIdFrom, albumIdFrom, isShortDeezerLink } from '../links.js';
  import { lang } from '../stores/settings.js';
  import { recordTypeLabel, tracksCount, longDuration, year } from '../format.js';
  import { RECORD_TYPES, MAX_AUDIO_BYTES, recordTypeFor, missingForPublish, prefillFromTags, createQueue, discsOf, withSeparators } from '../uploads.js';
  import ArtistPicker from '../components/upload/ArtistPicker.svelte';
  import TrackRows from '../components/upload/TrackRows.svelte';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import ImagePlus from '@lucide/svelte/icons/image-plus';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import X from '@lucide/svelte/icons/x';
  import Link2 from '@lucide/svelte/icons/link-2';

  pageTint().none();
  const id = $navStore.id;

  let loading = true;
  let error = '';
  // Поля релиза.
  let title = '';
  let artists = []; // [{ id, name, picture_url }]
  let recordType = 'single';
  let recordTouched = false; // выбрали тип сами — больше не подбирается по трекам
  let releaseDate = '';
  let coverUrl = '';
  let coverFromFile = false;
  let draft = true;
  let owner = ''; // кто загрузил — если правит не он (админ)
  let artistQuery = '';
  const touched = { title: false, releaseDate: false };
  let rows = []; // строки TrackRows: треки и разделители дисков ({ key, sep: true })
  let sepSeq = 0;
  const sepKey = () => `disc-${++sepSeq}`;
  // Что лежит на сервере (то же, что body() без published) — для «есть несохранённое».
  let server = null;
  let saving = false;
  let coverBusy = false;
  let coverInput;
  let coverOver = false;
  let showMissing = false; // нажали «Опубликовать», а чего-то не хватает — подсветить
  let versionOf = null; // { id, title, cover_url } основного релиза или null
  let versionLink = '';
  let versionError = '';
  let versionBusy = false;
  let trackLink = ''; // добавить существующий трек
  let trackLinkError = '';
  let trackLinkBusy = false;

  const ref = (a) => ({ id: String(a.id), name: a.name, picture_url: a.picture_url || null });

  function rowFrom(tr, prev) {
    return {
      key: prev?.key || tr.id,
      id: tr.id,
      title: tr.title,
      durationMs: tr.duration_ms,
      explicit: !!tr.explicit,
      artists: tr.artists ? tr.artists.map(ref) : null,
      other: !!tr.other, // с другого релиза: только порядок и «убрать»
      state: 'done',
      progress: 1,
      error: '',
      lyrics: prev?.lyrics,
      lyricsInitial: prev?.lyrics,
      open: !!prev?.open,
      busy: false,
    };
  }

  const trackBody = (r, disc) =>
    r.other
      ? { id: r.id, disc }
      : { id: r.id, title: r.title.trim(), artists: r.artists ? r.artists.map((a) => a.id) : null, explicit: r.explicit, disc };

  function fill(st) {
    title = st.title;
    artists = st.artists.map(ref);
    releaseDate = st.release_date || '';
    coverUrl = st.cover_url || '';
    draft = st.draft;
    owner = st.owner || '';
    versionOf = st.version_of || null;
    // Ещё не загруженные файлы — в конец; если их положили под новый диск,
    // которого на сервере пока нет, — вместе с его разделителем.
    const lastSep = rows.findLastIndex((r) => r.sep);
    const newDisc = lastSep >= 0 && rows.slice(lastSep + 1).every((r) => !r.id) ? [rows[lastSep]] : [];
    const pending = rows.filter((r) => !r.id && !r.sep);
    const saved = st.tracks.map((tr) => ({ ...rowFrom(tr, rows.find((r) => r.id === tr.id)), disc: tr.disc || 1 }));
    rows = [...withSeparators(saved, sepKey), ...newDisc, ...pending];
    const tracks = rows.filter((r) => !r.sep);
    recordType = st.record_type || recordTypeFor(tracks);
    recordTouched = st.tracks.length > 0 && recordType !== recordTypeFor(tracks);
    server = {
      title: st.title,
      artists: st.artists.map((a) => String(a.id)),
      record_type: recordType,
      release_date: st.release_date || '',
      version_of: st.version_of ? String(st.version_of.id) : '',
      tracks: st.tracks.map((tr) => trackBody(rowFrom(tr), tr.disc || 1)),
    };
  }

  // Тип — по трекам, пока его не выбрали руками.
  $: trackRows = rows.filter((r) => !r.sep);
  $: if (!loading && !recordTouched) recordType = recordTypeFor(trackRows);

  $: current = JSON.stringify({
    title: title.trim(),
    artists: artists.map((a) => a.id),
    record_type: recordType,
    release_date: releaseDate,
    version_of: versionOf ? String(versionOf.id) : '',
    tracks: discsOf(rows)
      .filter(({ row }) => row.id)
      .map(({ row, disc }) => trackBody(row, disc)),
  });
  $: lyricsDirty = rows.some((r) => r.lyrics !== undefined && r.lyrics !== r.lyricsInitial);
  $: dirty = !!server && (current !== JSON.stringify(server) || lyricsDirty);
  $: uploading = trackRows.some((r) => !r.id && r.state !== 'error');
  $: problems = missingForPublish({ title, artists, recordType, releaseDate, coverUrl, rows: trackRows });
  $: shownMissing = showMissing ? problems : [];
  $: if (showMissing && !problems.length) showMissing = false;
  $: setRouteTitle(`upload:${id}`, title.trim() || $t('upload.untitled'));

  // --- загрузка файлов ---
  const queue = createQueue(
    (item, onProgress, signal) => api.addReleaseAudio(id, item.file, { onProgress, signal }),
    () => syncQueue(),
  );

  function syncQueue() {
    for (const item of queue.items) {
      const row = rows.find((r) => r.key === item.key);
      if (!row || row.id) continue;
      row.state = item.state;
      row.progress = item.progress;
      row.error = item.error;
      if (item.state === 'done' && item.result) uploaded(row, item.result);
    }
    rows = rows;
  }

  function uploaded(row, { track, tags, cover_url }) {
    Object.assign(row, rowFrom(track, row));
    row.title = track.title;
    server.tracks = [...server.tracks, trackBody(row, track.disc || 1)]; // сервер добавил трек в конец последнего диска
    server = server;
    const pre = prefillFromTags({ title, artists }, tags, touched);
    if (pre.title) title = pre.title;
    if (pre.releaseDate) {
      releaseDate = pre.releaseDate;
      touched.releaseDate = true;
    }
    if (pre.artistQuery && !artistQuery && !artists.length) artistQuery = pre.artistQuery;
    if (cover_url && !coverUrl) {
      coverUrl = cover_url;
      coverFromFile = true;
    }
  }

  function addFiles(files) {
    const ok = [];
    for (const file of files) {
      if (file.size > MAX_AUDIO_BYTES) {
        toast($t('upload.tooBig', { name: file.name }), 'error');
        continue;
      }
      ok.push(file);
    }
    if (!ok.length) return;
    const before = queue.items.length;
    queue.add(ok);
    rows = [
      ...rows,
      ...queue.items.slice(before).map((item) => ({
        key: item.key, // по нему syncQueue находит строку
        id: null,
        title: item.file.name,
        durationMs: 0,
        explicit: false,
        artists: null,
        state: item.state,
        progress: 0,
        error: '',
        open: false,
      })),
    ];
    syncQueue();
  }

  function retryRow(row) {
    const item = queue.items.find((i) => i.key === row.key);
    if (item) queue.retry(item);
  }

  async function removeRow(row) {
    if (row.other) {
      rows = rows.filter((r) => r !== row); // сам трек остаётся, где был
      return;
    }
    if (!row.id) {
      const item = queue.items.find((i) => i.key === row.key);
      if (item && item.state !== 'error' && item.state !== 'queued') return toast($t('upload.waitUpload'));
      if (item) item.state = 'error'; // из очереди — не грузить
      rows = rows.filter((r) => r !== row);
      return;
    }
    if (!confirm($t('upload.deleteTrackConfirm', { title: row.title }))) return;
    try {
      await api.deleteUploadTrack(row.id);
      rows = rows.filter((r) => r !== row);
      server.tracks = server.tracks.filter((tr) => tr.id !== row.id);
      server = server;
      clearPageCaches();
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function replaceAudio({ row, file }) {
    if (file.size > MAX_AUDIO_BYTES) return toast($t('upload.tooBig', { name: file.name }), 'error');
    row.busy = true;
    rows = rows;
    try {
      const res = await api.replaceTrackAudio(row.id, file);
      row.durationMs = res.duration_ms;
      toast($t('upload.replaced'));
      clearPageCaches();
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      row.busy = false;
      rows = rows;
    }
  }

  // --- существующий трек и версия ---
  async function addExisting() {
    trackLinkError = '';
    if (isShortDeezerLink(trackLink)) return (trackLinkError = $t('edit.shortLink'));
    const tid = trackIdFrom(trackLink);
    if (!tid) return (trackLinkError = $t('edit.badTrackLink'));
    if (rows.some((r) => r.id === tid)) return (trackLinkError = $t('edit.trackExists'));
    trackLinkBusy = true;
    try {
      const tr = await api.discoverTrack(tid);
      const artists = tr.contributors?.length ? tr.contributors : [{ id: tr.artist_id, name: tr.artist }];
      rows = [...rows, rowFrom({ id: String(tr.id), title: tr.title, duration_ms: tr.duration_ms, explicit: tr.explicit, artists, other: true })];
      trackLink = '';
    } catch (e) {
      trackLinkError = e.message;
    } finally {
      trackLinkBusy = false;
    }
  }

  async function linkVersion() {
    versionError = '';
    if (isShortDeezerLink(versionLink)) return (versionError = $t('edit.shortLink'));
    const dz = albumIdFrom(versionLink);
    if (!dz) return (versionError = $t('edit.badAlbumLink'));
    if (dz === id) return (versionError = $t('upload.versionSelf'));
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

  // --- обложка ---
  async function setCover(file) {
    if (!file || !file.type.startsWith('image/')) return;
    coverBusy = true;
    try {
      const blob = await toSquareJpeg(file);
      const res = await api.uploadReleaseCover(id, blob);
      coverUrl = res.cover_url;
      coverFromFile = false;
      clearPageCaches();
    } catch (e) {
      toast(e.message === 'image too large' ? $t('upload.coverTooBig') : e.message, 'error');
    } finally {
      coverBusy = false;
    }
  }

  // --- сохранение ---
  function body(published) {
    const b = JSON.parse(current);
    if (published !== undefined) b.published = published;
    return b;
  }

  async function save(published) {
    if (published && problems.length) {
      showMissing = true;
      return;
    }
    saving = true;
    try {
      // Тексты — до релиза: публикация не должна обогнать их.
      for (const r of rows) {
        if (r.id && r.lyrics !== undefined && r.lyrics !== r.lyricsInitial) {
          await api.saveTrackLyrics(r.id, r.lyrics);
          r.lyricsInitial = r.lyrics;
        }
      }
      const st = await api.saveRelease(id, body(published));
      fill(st);
      clearPageCaches();
      if (published === true) {
        toast($t('upload.published'));
        await tick(); // dirty пересчитается — защита от ухода не спросит зря
        navigate(paths.album(id));
      } else toast($t(published === false ? 'upload.unpublished' : 'upload.saved'));
    } catch (e) {
      if (e.data?.missing?.length) showMissing = true; // сервер проверяет то же, что problems
      else toast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function deleteRelease() {
    if (!confirm($t('upload.deleteConfirm', { title: title.trim() || $t('upload.untitled') }))) return;
    try {
      await api.deleteRelease(id);
      unguard();
      queue.stop();
      clearPageCaches();
      toast($t('upload.deleted'));
      navigate(paths.uploads(), { replace: true });
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  function moreMenu(e) {
    openMenu(e, [
      ...(!draft ? [{ label: $t('upload.openRelease'), icon: ExternalLink, href: paths.album(id) }] : []),
      ...(!draft ? [{ label: $t('upload.unpublish'), icon: EyeOff, onClick: () => save(false) }] : []),
      { label: $t('upload.deleteRelease'), icon: Trash2, danger: true, onClick: deleteRelease },
    ]);
  }

  // --- уйти со страницы ---
  const unguard = guardLeave(() => !(dirty || uploading) || confirm($t('upload.leaveConfirm')));
  function beforeUnload(e) {
    if (dirty || uploading) e.preventDefault();
  }

  onMount(async () => {
    try {
      fill(await api.getRelease(id));
    } catch (e) {
      error = e.status === 404 ? $t('upload.notFound') : e.message;
    } finally {
      loading = false;
    }
  });
  onDestroy(() => {
    unguard();
    queue.stop();
  });

  // Сводка под обложкой: как релиз будет подписан на своей странице.
  $: totalSec = trackRows.reduce((sum, r) => sum + (r.id ? r.durationMs || 0 : 0), 0) / 1000;
  $: kindLine = [recordTypeLabel(recordType, $lang), year(releaseDate)].filter(Boolean).join(' · ');

  const missingText = (codes) => codes.map((c) => $t(`upload.missing.${c}`)).join(', ');
  $: bad = (code) => shownMissing.includes(code);
</script>

<svelte:window on:beforeunload={beforeUnload} />

<div class="page-pad upload">
  <a class="back" href={paths.uploads()} use:link><ArrowLeft size={18} /> {$t('upload.title')}</a>

  {#if error}
    <p class="error-text">{error}</p>
  {:else if loading}
    <div class="loading"><LoaderCircle class="spin" size={24} /></div>
  {:else}
    <header class="head">
      <div class="head-text">
        <h1>{title.trim() || $t('upload.untitled')}</h1>
        <p class="legend">{$t('upload.legend')}</p>
      </div>
      <div class="chips">
        <span class="state" class:live={!draft}>{$t(draft ? 'upload.draftNote' : 'upload.liveNote')}</span>
        {#if owner}<span class="state">{$t('upload.by', { name: owner })}</span>{/if}
      </div>
    </header>

    <div class="layout">
      <!-- Слева — обложка и сводка, как релиз будет подписан; на широком
           экране она остаётся на месте, пока правишь треки. -->
      <aside class="side">
        <span class="label">{$t('upload.cover')} <b>*</b></span>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="cover"
          class:over={coverOver}
          class:invalid={bad('cover')}
          class:filled={!!coverUrl}
          on:dragover|preventDefault={() => (coverOver = true)}
          on:dragleave={() => (coverOver = false)}
          on:drop|preventDefault={(e) => ((coverOver = false), setCover(e.dataTransfer?.files?.[0]))}
        >
          <button type="button" class="cover-btn" aria-label={$t(coverUrl ? 'upload.changeCover' : 'upload.cover')} on:click={() => coverInput.click()}>
            {#if coverUrl}
              <img src={coverUrl} alt="" />
              <span class="cover-overlay"><ImagePlus size={28} />{$t('upload.changeCover')}</span>
            {:else}
              <span class="cover-empty"><ImagePlus size={36} /><span>{$t('upload.coverDrop')}</span></span>
            {/if}
            {#if coverBusy}<span class="cover-busy"><LoaderCircle class="spin" size={28} /></span>{/if}
          </button>
        </div>
        <input bind:this={coverInput} type="file" accept="image/*" hidden on:change={(e) => (setCover(e.currentTarget.files[0]), (e.currentTarget.value = ''))} />
        <div class="summary">
          <small>{$t(coverFromFile ? 'upload.coverFromFile' : 'upload.coverHint')}</small>
          {#if title.trim() || artists.length || trackRows.length}
          <div class="summary-card">
            <span class="kind">{kindLine}</span>
            <strong class="sum-title">{title.trim() || $t('upload.untitled')}</strong>
            <span class="sum-artists">{artists.map((a) => a.name).join(', ') || '—'}</span>
            <span class="sum-meta">{tracksCount(trackRows.filter((r) => r.id).length, $lang)}{#if totalSec}, {longDuration(totalSec, $lang)}{/if}</span>
          </div>
          {/if}
        </div>
      </aside>

      <div class="sections">
        <section class="card">
          <h2>{$t('upload.details')}</h2>
          <label class="field">
            <span class="label">{$t('upload.releaseTitle')} <b>*</b></span>
            <input
              type="text"
              maxlength="300"
              class:invalid={bad('title')}
              placeholder={$t('upload.titlePlaceholder')}
              bind:value={title}
              on:input={() => (touched.title = true)}
            />
          </label>
          <div class="field">
            <label class="label" for="release-artists">{$t('upload.artists')} <b>*</b></label>
            <ArtistPicker bind:artists bind:query={artistQuery} invalid={bad('artists')} inputId="release-artists" />
            <small>{$t('upload.artistsHint')}</small>
          </div>
          <div class="row2">
            <div class="field">
              <span class="label" id="release-type">{$t('upload.type')} <b>*</b></span>
              <div class="segmented" role="radiogroup" aria-labelledby="release-type">
                {#each RECORD_TYPES as type}
                  <button type="button" role="radio" aria-checked={recordType === type} class:active={recordType === type}
                    on:click={() => ((recordType = type), (recordTouched = true))}>{$t(`type.${type}`)}</button>
                {/each}
              </div>
              {#if !recordTouched}<small>{$t('upload.typeAuto')}</small>{/if}
            </div>
            <label class="field">
              <span class="label">{$t('upload.date')} <b>*</b></span>
              <input type="date" class:invalid={bad('release_date')} bind:value={releaseDate} on:input={() => (touched.releaseDate = true)} />
            </label>
          </div>
        </section>

        <section class="card">
          <h2>{$t('upload.tracks')} <b>*</b>{#if trackRows.length}<span class="count">{trackRows.length}</span>{/if}</h2>
          <TrackRows
            bind:rows
            releaseArtists={artists}
            invalid={bad('tracks')}
            invalidTitles={bad('track_titles')}
            on:files={(e) => addFiles(e.detail)}
            on:rejected={() => toast($t('upload.notAudio'), 'error')}
            on:retry={(e) => retryRow(e.detail)}
            on:remove={(e) => removeRow(e.detail)}
            on:replace={(e) => replaceAudio(e.detail)}
          />
          <div class="field existing">
            <label class="label sub" for="existing-track"><Link2 size={16} /> {$t('upload.addExisting')}</label>
            <div class="link-row">
              <input id="existing-track" type="text" bind:value={trackLink} placeholder={$t('edit.trackLink')}
                on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), addExisting())} />
              <button type="button" class="pill outline" disabled={!trackLink.trim() || trackLinkBusy} on:click={addExisting}>
                {#if trackLinkBusy}<LoaderCircle class="spin" size={16} />{/if}{$t('upload.add')}
              </button>
            </div>
            {#if trackLinkError}<p class="error-text">{trackLinkError}</p>{/if}
            <small>{$t('upload.addExistingHint')}</small>
          </div>
        </section>

        <section class="card">
          <h2>{$t('edit.versionOf')} <span class="optional">{$t('upload.optional')}</span></h2>
          <div class="field">
            {#if versionOf}
              <div class="linked">
                {#if versionOf.cover_url}<img src={sized(versionOf.cover_url, 64)} alt="" />{/if}
                <span class="linked-title">{versionOf.title}</span>
                <button type="button" class="icon-btn" title={$t('edit.unlinkVersion')} aria-label={$t('edit.unlinkVersion')} on:click={() => (versionOf = null)}><X size={18} /></button>
              </div>
            {:else}
              <div class="link-row">
                <input type="text" bind:value={versionLink} placeholder={$t('edit.albumLink')} aria-label={$t('edit.versionOf')}
                  on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), linkVersion())} />
                <button type="button" class="pill outline" disabled={!versionLink.trim() || versionBusy} on:click={linkVersion}>
                  {#if versionBusy}<LoaderCircle class="spin" size={16} />{/if}{$t('edit.linkVersion')}
                </button>
              </div>
            {/if}
            {#if versionError}<p class="error-text">{versionError}</p>{/if}
            <small>{$t('upload.versionHint')}</small>
          </div>
        </section>
      </div>
    </div>
  {/if}
</div>

{#if !loading && !error}
  <div class="savebar">
    <div class="savebar-in page-pad">
      <p class="status" class:warn={shownMissing.length}>
        {#if shownMissing.length}
          {$t('upload.missingPrefix', { list: missingText(shownMissing) })}
        {:else if uploading}
          {$t('upload.uploadingFiles')}
        {:else if dirty}
          {$t('upload.unsaved')}
        {:else}
          {$t(draft ? 'upload.savedDraft' : 'upload.savedLive')}
        {/if}
      </p>
      <div class="buttons">
        <button type="button" class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.more')} on:click={moreMenu}><Ellipsis size={22} /></button>
        <button type="button" class="pill outline" disabled={!dirty || saving} on:click={() => save()}>
          {#if saving}<LoaderCircle class="spin" size={16} />{/if}{$t('upload.save')}
        </button>
        {#if draft}
          <button type="button" class="pill publish" disabled={saving} on:click={() => save(true)}>{$t('upload.publish')}</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .upload {
    --upload-w: 1120px;
    padding-top: 16px;
    padding-bottom: 32px;
    max-width: calc(var(--upload-w) + 64px); /* + поля page-pad */
    margin: 0 auto;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .back:hover {
      color: var(--text);
    }
  }
  .loading {
    padding: 48px 0;
    color: var(--text-dim);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px 24px;
    margin: 16px 0 28px;
  }
  .head-text {
    min-width: 0;
  }
  h1 {
    font-size: 32px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .legend {
    margin: 8px 0 0;
    max-width: 640px;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-faint);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
    flex-shrink: 0;
    padding-top: 6px;
  }
  .state {
    padding: 5px 12px;
    border-radius: 999px;
    background: var(--bg-press);
    font-size: 13px;
    color: var(--text-dim);
    white-space: nowrap;
  }
  .state.live {
    background: rgba(30, 215, 96, 0.15);
    color: var(--ok);
  }
  b {
    color: var(--danger);
    font-weight: 700;
  }

  /* --- раскладка: обложка слева, разделы справа --- */
  .layout {
    display: grid;
    grid-template-columns: 260px minmax(0, 1fr);
    gap: 32px;
    align-items: start;
  }
  .side {
    position: sticky;
    top: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .sections {
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-width: 0;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 22px;
    padding: 24px;
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.035);
    border: 1px solid rgba(255, 255, 255, 0.06);
  }
  h2 {
    font-size: 18px;
    line-height: 1.3;
  }
  .count {
    margin-left: 6px;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
  }
  .optional {
    margin-left: 4px;
    font: 500 13px var(--font);
    color: var(--text-faint);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .label {
    font-size: 14px;
    font-weight: 600;
  }
  .label.sub {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-dim);
  }
  small {
    font-size: 12px;
    line-height: 1.45;
    color: var(--text-faint);
  }

  /* --- обложка и сводка --- */
  .cover {
    width: 100%;
    aspect-ratio: 1;
    border-radius: 8px;
    border: 2px dashed #4a4a4a;
    overflow: hidden;
    transition: border-color 0.15s;
  }
  .cover.filled {
    border-style: solid;
    border-color: transparent;
    box-shadow: 0 8px 28px rgba(0, 0, 0, 0.45);
  }
  .cover.over {
    border-color: var(--text);
  }
  .cover.invalid {
    border-color: var(--danger);
  }
  .cover-btn {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .cover-empty,
  .cover-overlay,
  .cover-busy {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px;
    text-align: center;
    font-size: 13px;
    color: var(--text-dim);
  }
  .cover-overlay {
    background: rgba(0, 0, 0, 0.6);
    color: var(--text);
    opacity: 0;
    transition: opacity 0.15s;
  }
  .cover-btn:focus-visible .cover-overlay {
    opacity: 1;
  }
  @media (hover: hover) {
    .cover-btn:hover .cover-overlay {
      opacity: 1;
    }
    .cover:not(.filled):hover {
      border-color: #6a6a6a;
    }
  }
  .cover-busy {
    background: rgba(0, 0, 0, 0.6);
    color: var(--text);
  }
  .summary {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .summary-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }
  .kind {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-dim);
  }
  .sum-title {
    font: 700 17px/1.3 var(--font-display);
    overflow-wrap: anywhere;
  }
  .sum-artists,
  .sum-meta {
    font-size: 13px;
    color: var(--text-dim);
    overflow-wrap: anywhere;
  }
  .sum-meta {
    color: var(--text-faint);
  }

  /* --- поля --- */
  .row2 {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(160px, 220px);
    gap: 20px;
    align-items: start;
  }
  input[type='text'],
  input[type='date'] {
    height: 42px;
    padding: 0 12px;
    border-radius: 6px;
    border: 1px solid transparent;
    background: #3a3a3a;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
    color-scheme: dark;
  }
  input:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  input.invalid {
    border-color: var(--danger);
  }
  .segmented {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 4px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.25);
    width: fit-content;
  }
  .segmented button {
    padding: 7px 14px;
    border-radius: 999px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .segmented button.active {
    background: var(--text);
    color: var(--bg);
  }
  .link-row {
    display: flex;
    gap: 8px;
  }
  .link-row input {
    flex: 1;
    min-width: 0;
  }
  .link-row .pill {
    height: 42px;
    padding: 0 18px;
    font-weight: 700;
  }
  .existing {
    padding-top: 20px;
    border-top: 1px solid var(--border);
  }
  .linked {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 56px;
    padding: 6px 6px 6px 8px;
    border-radius: 8px;
    background: #2e2e2e;
  }
  .linked img {
    width: 44px;
    height: 44px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }
  .linked-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
  }

  /* --- панель сохранения --- */
  .savebar {
    position: sticky;
    bottom: 0;
    z-index: 4;
    margin-top: 8px;
    background: rgba(18, 18, 18, 0.94);
    backdrop-filter: blur(8px);
    /* Фон и линия сверху — во всю ширину, хотя панель в колонке страницы
       (App.svelte): тени без размытия продолжают их в стороны, clip-path
       оставляет только высоту панели и линию над ней. */
    box-shadow: 0 0 0 100vmax rgba(18, 18, 18, 0.94);
    clip-path: inset(-1px -100vmax 0);
  }
  /* Линия — отдельным слоем высотой 1 px: тенью под фоном она просвечивала
     сквозь его прозрачность полосами по бокам, где страница шире колонки. */
  .savebar::before {
    content: '';
    position: absolute;
    top: -1px;
    left: 0;
    right: 0;
    height: 1px;
    background: var(--border);
    box-shadow: 0 0 0 100vmax var(--border);
    clip-path: inset(0 -100vmax);
  }
  .savebar-in {
    display: flex;
    align-items: center;
    gap: 16px;
    padding-top: 12px;
    padding-bottom: 12px;
    max-width: calc(1120px + 64px);
    margin: 0 auto;
  }
  .status {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  .status.warn {
    color: var(--danger);
  }
  .buttons {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .buttons .pill {
    height: 40px;
    padding: 0 20px;
    font-weight: 700;
  }
  .publish {
    background: var(--text);
    color: var(--bg);
  }
  @media (hover: hover) {
    .publish:hover:not(:disabled) {
      background: var(--text);
      transform: scale(1.04);
    }
  }
  .pill:disabled {
    opacity: 0.5;
  }

  /* На телефоне панель — над нижним меню и мини-плеером (App.svelte),
     а не под ними. */
  @media (max-width: 768px) {
    .savebar {
      bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    }
    :global(.shell.with-player) .savebar {
      bottom: calc(var(--tabbar-h) + 76px + env(safe-area-inset-bottom));
    }
  }

  /* Узко: обложка со сводкой — строкой над разделами. */
  @media (max-width: 900px) {
    .layout {
      grid-template-columns: minmax(0, 1fr);
      gap: 20px;
    }
    .side {
      position: static;
      display: grid;
      grid-template-columns: 160px minmax(0, 1fr);
      gap: 8px 16px;
      align-items: start;
    }
    .side > .label {
      grid-column: 1 / -1;
    }
    .summary-card {
      padding-top: 0;
      border-top: 0;
      order: -1;
    }
  }
  @media (max-width: 720px) {
    .head {
      flex-direction: column;
      gap: 12px;
      margin-bottom: 20px;
    }
    .chips {
      justify-content: flex-start;
      padding-top: 0;
    }
    h1 {
      font-size: 24px;
    }
    .card {
      padding: 16px;
      gap: 18px;
    }
    .row2 {
      grid-template-columns: minmax(0, 1fr);
    }
    .side {
      grid-template-columns: 120px minmax(0, 1fr);
    }
    .savebar-in {
      flex-wrap: wrap;
      gap: 8px;
    }
    .status {
      flex-basis: 100%;
    }
    .buttons {
      width: 100%;
      justify-content: flex-end;
    }
    /* «Мини-альбом» длинный: четыре типа должны влезть в одну строку */
    .segmented button {
      padding: 6px 10px;
      font-size: 13px;
    }
  }
</style>
