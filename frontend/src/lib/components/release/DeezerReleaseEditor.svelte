<script>
  // Правка релиза из Deezer (/album/<id>/edit, только админ) в том же
  // каркасе, что и свой релиз (EditShell): правки ложатся поверх данных
  // Deezer (internal/api/overlay.go). Обложка, название, исполнители, тип,
  // дата, треклист (порядок, замена и добавление треков по ссылке, правка
  // трека — карандашом в строке) и «версия чего» сохраняются кнопкой
  // «Сохранить»; скрыть / показать, обновить из Deezer и сбросить правки —
  // сразу, из меню «…». Запись релиза в медиатеке заводится при открытии
  // (claim), как раньше в окне правки.
  import { onMount, onDestroy, tick } from 'svelte';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { paths, guardLeave } from '../../stores/nav.js';
  import { toast } from '../../stores/toast.js';
  import { openMenu } from '../../stores/contextMenu.js';
  import { setRouteTitle } from '../../stores/title.js';
  import { pageTint } from '../../stores/theme.js';
  import { clearPageCaches } from '../../lru.js';
  import { toSquareJpeg } from '../../image.js';
  import { lang } from '../../stores/settings.js';
  import { recordTypeLabel, tracksCount, longDuration, year } from '../../format.js';
  import { createNewArtists } from '../../uploads.js';
  import EditShell from './EditShell.svelte';
  import ReleaseSide from './ReleaseSide.svelte';
  import ReleaseDetails from './ReleaseDetails.svelte';
  import VersionField from './VersionField.svelte';
  import DeezerTrackRows, { rowFromTrack } from './DeezerTrackRows.svelte';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import ExternalLink from '@lucide/svelte/icons/external-link';

  export let id; // Deezer ID релиза

  pageTint().none();

  let loading = true;
  let error = '';
  let libId = null; // запись в медиатеке (claim)
  let item = null;
  let title = '';
  let artists = [];
  let artistQuery = '';
  let recordType = 'album';
  let releaseDate = '';
  let hidden = false;
  let versionOf = null;
  let rows = null; // треклист; null — Deezer не ответил, без него
  let server = ''; // что на сервере — для «есть несохранённое»
  let newImage = null; // Blob — уйдёт при сохранении
  let newImageUrl = null;
  let saving = false;
  let editingTrack = null;

  const ref = (a) => ({ id: String(a.id), name: a.name, picture_url: a.picture_url || null });
  const ids = (list) => list.map((a) => a.id);

  $: current = JSON.stringify({
    title: title.trim(),
    artists: ids(artists),
    record_type: recordType,
    released_at: releaseDate,
    version_of: versionOf ? String(versionOf.id) : '',
    tracklist: rows ? rows.map((r) => r.id) : null,
  });
  $: dirty = !!server && (current !== server || !!newImage);
  $: setRouteTitle(`releaseEdit:${id}`, title.trim() || id);

  async function load() {
    try {
      if (!libId) libId = (await api.claim('album', id)).id;
      item = await api.getAlbum(libId);
      title = item.title;
      artists = (item.artists || []).map(ref);
      recordType = item.record_type || 'album';
      releaseDate = item.released_at || '';
      hidden = !!item.hidden;
      versionOf = null;
      if (item.version_of) {
        versionOf = { id: item.version_of, title: item.version_of };
        api.discoverAlbum(item.version_of).then(
          ({ album }) => (versionOf = album),
          () => {},
        );
      }
      await loadTracks();
      dropImage();
      await tick();
      server = current;
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function loadTracks() {
    try {
      const { tracks } = await api.discoverAlbum(id);
      rows = tracks.map((tr) => rowFromTrack(tr, id));
    } catch {
      rows = null; // Deezer недоступен — остальное править можно
    }
  }

  function dropImage() {
    if (newImageUrl) URL.revokeObjectURL(newImageUrl);
    newImage = newImageUrl = null;
  }

  async function pickImage(file) {
    try {
      const blob = await toSquareJpeg(file);
      dropImage();
      newImage = blob;
      newImageUrl = URL.createObjectURL(blob);
    } catch {
      toast($t('edit.badImage'), 'error');
    }
  }

  // Убрать обложку: новую, ещё не сохранённую, — просто забыть; свою
  // сохранённую — удалить на сервере, и снова видна обложка Deezer.
  async function removeCover() {
    if (newImage) return dropImage();
    if (!confirm($t('edit.removeCoverConfirm'))) return;
    try {
      item = await api.deleteAlbumCover(libId);
      clearPageCaches();
      toast($t('upload.coverRemoved'));
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function save() {
    if (!title.trim()) return toast($t('edit.nameRequired'), 'error');
    if (!artists.length) return toast($t('upload.missingPrefix', { list: $t('upload.missing.artists') }), 'error');
    saving = true;
    try {
      // Новые артисты заводятся только при сохранении.
      [artists] = await createNewArtists([artists], api.createArtist);
      await tick();
      const was = JSON.parse(server);
      const now = JSON.parse(current);
      // Исполнители, треклист и версия — только если их меняли.
      const fields = { title: now.title, released_at: now.released_at, record_type: now.record_type };
      if (now.artists.join() !== was.artists.join()) fields.artists = now.artists;
      if (now.tracklist && now.tracklist.join() !== was.tracklist?.join()) fields.tracklist = now.tracklist;
      if (now.version_of !== was.version_of) fields.version_of = now.version_of;
      await api.updateAlbum(libId, fields);
      if (newImage) await api.uploadAlbumCover(libId, newImage);
      clearPageCaches(); // релиз мог переехать к другому артисту — его страницы в кеше устарели
      await load();
      toast($t('upload.saved'));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      saving = false;
    }
  }

  async function toggleHidden() {
    try {
      const res = await api.setAlbumHidden(id, !hidden);
      hidden = res.hidden;
      clearPageCaches();
      toast($t(hidden ? 'admin.releaseHidden' : 'admin.releaseShown'));
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function refreshFromDeezer() {
    if (dirty && !confirm($t('upload.leaveConfirm'))) return;
    try {
      await api.refreshAlbum(id);
      clearPageCaches();
      await load();
      toast($t('admin.releaseRefreshed'));
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function reset() {
    if (!confirm($t('edit.resetConfirm'))) return;
    try {
      await api.resetAlbum(libId);
      clearPageCaches();
      await load();
      toast($t('edit.resetDone'));
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  function moreMenu(e) {
    openMenu(e, [
      { label: $t('upload.openRelease'), icon: ExternalLink, href: paths.album(id) },
      { label: $t(hidden ? 'admin.showRelease' : 'admin.hideRelease'), icon: hidden ? Eye : EyeOff, onClick: toggleHidden },
      { label: $t('admin.refreshRelease'), icon: RefreshCw, onClick: refreshFromDeezer },
      ...(item?.edited || item?.cover_custom ? [{ separator: true }, { label: $t('edit.reset'), icon: RotateCcw, danger: true, onClick: reset }] : []),
    ]);
  }

  // Трек поправили в его окне: перечитать строку (а весь треклист — если
  // порядок здесь не трогали: замена трека в окне меняет и его).
  async function trackSaved(trackId) {
    editingTrack = null;
    clearPageCaches();
    if (rows && JSON.stringify(rows.map((r) => r.id)) === JSON.stringify(JSON.parse(server).tracklist)) {
      await loadTracks();
      server = JSON.stringify({ ...JSON.parse(server), tracklist: rows?.map((r) => r.id) ?? null });
      return;
    }
    try {
      const row = rowFromTrack(await api.discoverTrack(trackId), id);
      rows = rows.map((r) => (r.id === trackId ? row : r));
    } catch {
      // не страшно: обновится при сохранении
    }
  }

  const unguard = guardLeave(() => !dirty || confirm($t('upload.leaveConfirm')));
  function beforeUnload(e) {
    if (dirty) e.preventDefault();
  }
  onMount(load);
  onDestroy(() => {
    unguard();
    dropImage();
  });

  $: totalSec = (rows || []).reduce((sum, r) => sum + r.durationMs, 0) / 1000;
  $: kindLine = [recordTypeLabel(recordType, $lang), year(releaseDate)].filter(Boolean).join(' · ');
  $: coverUrl = newImageUrl || item?.image_url || '';
</script>

<svelte:window on:beforeunload={beforeUnload} />

<EditShell backHref={paths.album(id)} backLabel={$t('edit.backToRelease')} title={title.trim() || item?.title || ''} legend={$t('edit.deezerLegend')} {loading} {error}>
  <svelte:fragment slot="chips">
    <span class="state">Deezer</span>
    {#if item?.edited || item?.cover_custom}<span class="state">{$t('edit.editedChip')}</span>{/if}
    {#if hidden}<span class="state warn"><EyeOff size={14} /> {$t('edit.hiddenChip')}</span>{/if}
  </svelte:fragment>

  <svelte:fragment slot="side">
    <ReleaseSide
      {coverUrl}
      hint={$t(newImage ? 'edit.coverPending' : 'edit.coverHint')}
      {kindLine}
      title={title.trim()}
      artists={artists.map((a) => a.name).join(', ')}
      meta={rows ? `${tracksCount(rows.length, $lang)}${totalSec ? `, ${longDuration(totalSec, $lang)}` : ''}` : ''}
      removeLabel={newImage ? $t('edit.dropNewCover') : item?.cover_custom ? $t('edit.removeCover') : ''}
      on:file={(e) => pickImage(e.detail)}
      on:remove={removeCover}
    />
  </svelte:fragment>

  <ReleaseDetails bind:title bind:artists bind:artistQuery bind:recordType bind:releaseDate />

  <section class="card">
    <h2>{$t('upload.tracks')}{#if rows?.length}<span class="count">{rows.length}</span>{/if}</h2>
    {#if rows}
      <DeezerTrackRows bind:rows albumId={id} on:edit={(e) => (editingTrack = e.detail.id)} />
    {:else}
      <p class="muted">{$t('edit.noTracklist')}</p>
    {/if}
  </section>

  <VersionField bind:versionOf selfId={id} hint={$t('edit.versionHint')} />

  <svelte:fragment slot="bar">
    <p class="status">{$t(dirty ? 'upload.unsaved' : 'edit.allSaved')}</p>
    <div class="buttons">
      <button type="button" class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.more')} on:click={moreMenu}><Ellipsis size={22} /></button>
      <button type="button" class="pill primary" disabled={!dirty || saving} on:click={save}>
        {#if saving}<LoaderCircle class="spin" size={16} />{/if}{$t('upload.save')}
      </button>
    </div>
  </svelte:fragment>
</EditShell>

{#if editingTrack}
  {#await import('../EditDialog.svelte') then m}
    <svelte:component this={m.default} kind="track" deezerId={editingTrack} albumId={id} on:close={() => (editingTrack = null)} on:saved={() => trackSaved(editingTrack)} />
  {/await}
{/if}

<style>
  .muted {
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
</style>
