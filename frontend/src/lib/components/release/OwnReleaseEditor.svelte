<script>
  import { onMount, onDestroy, tick } from 'svelte';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { paths, navigate, guardLeave } from '../../stores/nav.js';
  import { toast } from '../../stores/toast.js';
  import { openMenu } from '../../stores/contextMenu.js';
  import { setRouteTitle } from '../../stores/title.js';
  import { pageTint } from '../../stores/theme.js';
  import { clearPageCaches } from '../../lru.js';
  import { toSquareJpeg } from '../../image.js';
  import { trackIdFrom, isShortDeezerLink } from '../../links.js';
  import { lang } from '../../stores/settings.js';
  import { recordTypeLabel, tracksCount, longDuration, year } from '../../format.js';
  import { MAX_AUDIO_BYTES, recordTypeFor, missingForPublish, prefillFromTags, createQueue, discsOf, withSeparators, createNewArtists } from '../../uploads.js';
  import TrackRows from '../upload/TrackRows.svelte';
  import EditShell from './EditShell.svelte';
  import ReleaseSide from './ReleaseSide.svelte';
  import ReleaseDetails from './ReleaseDetails.svelte';
  import VersionField from './VersionField.svelte';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import Link2 from '@lucide/svelte/icons/link-2';

  export let id;

  pageTint().none();

  let loading = true;
  let error = '';
  let title = '';
  let artists = [];
  let recordType = 'single';
  let recordTouched = false;
  let releaseDate = '';
  let coverUrl = '';
  let coverFromFile = false;
  let draft = true;
  let owner = '';
  let artistQuery = '';
  const touched = { title: false, releaseDate: false };
  let rows = [];
  let sepSeq = 0;
  const sepKey = () => `disc-${++sepSeq}`;
  let server = null;
  let saving = false;
  let coverBusy = false;
  let showMissing = false;
  let versionOf = null;
  let trackLink = '';
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
      other: !!tr.other,
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
  $: problems = missingForPublish({ title, artists, recordType, releaseDate, rows: trackRows });
  $: shownMissing = showMissing ? problems : [];
  $: if (showMissing && !problems.length) showMissing = false;
  $: setRouteTitle(`releaseEdit:${id}`, title.trim() || $t('upload.untitled'));

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
    server.tracks = [...server.tracks, trackBody(row, track.disc || 1)];
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
        key: item.key,
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
      rows = rows.filter((r) => r !== row);
      return;
    }
    if (!row.id) {
      const item = queue.items.find((i) => i.key === row.key);
      if (item && item.state !== 'error' && item.state !== 'queued') return toast($t('upload.waitUpload'));
      if (item) item.state = 'error';
      rows = rows.filter((r) => r !== row);
      return;
    }
    const rest = trackRows.filter((r) => r !== row);
    if (!draft && rest.length && !server.tracks.some((tr) => tr.id !== row.id)) return toast($t('upload.saveFirst'));
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

  async function setCover(file) {
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

  async function removeCover() {
    if (!confirm($t(draft ? 'upload.removeCoverConfirm' : 'upload.removeCoverConfirmLive'))) return;
    coverBusy = true;
    try {
      const res = await api.deleteReleaseCover(id);
      coverUrl = res.cover_url || '';
      coverFromFile = false;
      clearPageCaches();
      toast($t('upload.coverRemoved'));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      coverBusy = false;
    }
  }

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
      for (const r of rows) {
        if (r.id && r.lyrics !== undefined && r.lyrics !== r.lyricsInitial) {
          await api.saveTrackLyrics(r.id, r.lyrics);
          r.lyricsInitial = r.lyrics;
        }
      }
      const own = rows.filter((r) => !r.sep && r.artists);
      const [release, ...lists] = await createNewArtists([artists, ...own.map((r) => r.artists)], api.createArtist);
      artists = release;
      own.forEach((r, i) => (r.artists = lists[i]));
      rows = rows;
      await tick();
      const st = await api.saveRelease(id, body(published));
      fill(st);
      clearPageCaches();
      if (published === true) {
        toast($t('upload.published'));
        await tick();
        navigate(paths.album(id));
      } else toast($t(published === false ? 'upload.unpublished' : 'upload.saved'));
    } catch (e) {
      if (e.data?.missing?.length) showMissing = true;
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

  $: totalSec = trackRows.reduce((sum, r) => sum + (r.id ? r.durationMs || 0 : 0), 0) / 1000;
  $: kindLine = [recordTypeLabel(recordType, $lang), year(releaseDate)].filter(Boolean).join(' · ');

  const missingText = (codes) => codes.map((c) => $t(`upload.missing.${c}`)).join(', ');
  $: bad = (code) => shownMissing.includes(code);
  $: hasSummary = !!(title.trim() || artists.length || trackRows.length);
</script>

<svelte:window on:beforeunload={beforeUnload} />

<EditShell backHref={paths.uploads()} backLabel={$t('upload.title')} title={title.trim() || $t('upload.untitled')} legend={$t('upload.legend')} {loading} {error}>
  <svelte:fragment slot="chips">
    <span class="state" class:live={!draft}>{$t(draft ? 'upload.draftNote' : 'upload.liveNote')}</span>
    {#if owner}<span class="state">{$t('upload.by', { name: owner })}</span>{/if}
  </svelte:fragment>

  <svelte:fragment slot="side">
    <ReleaseSide
      {coverUrl}
      busy={coverBusy}
      placeholderFor={id}
      optional
      hint={$t(coverFromFile ? 'upload.coverFromFile' : coverUrl ? 'upload.coverHint' : 'upload.coverPlaceholder')}
      {kindLine}
      title={hasSummary ? title.trim() || $t('upload.untitled') : ''}
      artists={hasSummary ? artists.map((a) => a.name).join(', ') : ''}
      meta={`${tracksCount(trackRows.filter((r) => r.id).length, $lang)}${totalSec ? `, ${longDuration(totalSec, $lang)}` : ''}`}
      removeLabel={$t('upload.removeCover')}
      on:file={(e) => setCover(e.detail)}
      on:remove={removeCover}
    />
  </svelte:fragment>

  <ReleaseDetails
    bind:title
    bind:artists
    bind:artistQuery
    bind:recordType
    bind:releaseDate
    required
    typeAuto={!recordTouched}
    invalid={shownMissing}
    on:title={() => (touched.title = true)}
    on:date={() => (touched.releaseDate = true)}
    on:type={() => (recordTouched = true)}
  />

  <section class="card">
    <h2>{$t('upload.tracks')} <b class="req">*</b>{#if trackRows.length}<span class="count">{trackRows.length}</span>{/if}</h2>
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

  <VersionField bind:versionOf selfId={id} hint={$t('upload.versionHint')} />

  <svelte:fragment slot="bar">
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
        <button type="button" class="pill primary" disabled={saving} on:click={() => save(true)}>{$t('upload.publish')}</button>
      {/if}
    </div>
  </svelte:fragment>
</EditShell>

<style>
  .existing {
    padding-top: 20px;
    border-top: 1px solid var(--border);
  }
</style>
