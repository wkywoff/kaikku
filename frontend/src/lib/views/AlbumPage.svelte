<script context="module">
  import { LRU } from '../lru.js';
  // Кеш между заходами: «назад» показывает страницу сразу.
  const cache = new LRU(30);
</script>

<script>
  // Страница альбома из каталога Deezer (/album/<id>), с ручными правками
  // админа поверх, или своего релиза (k…, загрузка): его правят автор и
  // админ в окне загрузки (/upload/<id>), а черновик видят только они.
  import { sized } from '../imageproxy.js';
  import { api } from '../api.js';
  import { player, isPlaying as playerPlaying, shuffleOn } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { refresh, playRows, playingContext, isContext, queueRow } from '../library.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { toast } from '../stores/toast.js';
  import { shareItem } from '../share.js';
  import { savedAlbumIds, toggleSaveAlbum } from '../stores/collection.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import { recordTypeLabel, tracksCount, longDuration, year, longDate } from '../format.js';
  import Hero from '../components/Hero.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import CoverLightbox from '../components/CoverLightbox.svelte';
  import MoreAlbums from '../components/MoreAlbums.svelte';
  import AlbumVersions from '../components/AlbumVersions.svelte';
  import { setRouteTitle } from '../stores/title.js';
  import { canEdit } from '../stores/me.js';
  import { isLocalId } from '../links.js';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import CirclePlus from '@lucide/svelte/icons/circle-plus';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import UserRound from '@lucide/svelte/icons/user-round';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import ListEnd from '@lucide/svelte/icons/list-end';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import Eye from '@lucide/svelte/icons/eye';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';

  const tint = pageTint();

  // album: { deezerId, libraryId?, title, type, coverUrl, artists: [{id, name, href, picture}], releaseDate, label, durationSec, href }
  let album = null;
  let rows = [];
  let loading = true;
  let error = '';
  let showCover = false;
  let editing = false;

  // Перезагрузить без кеша — после правки.
  function reload() {
    cache.delete($navStore.id);
    load($navStore.id);
  }

  $: load($navStore.id);

  let seq = 0;
  async function load(id) {
    const my = ++seq;
    error = '';
    const cached = cache.get(id);
    if (cached) apply(cached);
    else {
      album = null;
      rows = [];
    }
    loading = !cached;
    try {
      const data = await loadDeezer(id);
      if (my !== seq) return;
      cache.set(id, data);
      apply(data);
    } catch (e) {
      if (my === seq) error = e.message;
    } finally {
      if (my === seq) loading = false;
    }
  }

  async function loadDeezer(id) {
    const { album: a, tracks, versions, draft, editable } = await api.discoverAlbum(id);
    const contributors = a.contributors?.length ? a.contributors : [{ id: a.artist_id, name: a.artist_name }];
    return {
      album: {
        deezerId: a.id,
        libraryId: a.library_id || null,
        title: a.title,
        type: a.record_type,
        coverUrl: a.cover_url,
        artists: contributors.map((c) => ({ id: c.id, name: c.name, href: paths.artist(c.id), picture: c.picture_url || null })),
        releaseDate: a.release_date,
        label: a.label,
        durationSec: a.duration_sec,
        href: paths.album(a.id),
        hidden: !!a.hidden,
        explicit: !!a.explicit,
        versions: versions || [],
        own: isLocalId(a.id), // свой релиз (загрузка)
        draft: !!draft,
        editable: !!editable,
      },
      rows: tracks.map((tr) => rowFromDeezerTrack(tr, a)),
    };
  }

  function apply(data) {
    album = data.album;
    rows = data.rows;
    tint.from(album.coverUrl);
    refresh(rows.map((r) => r.deezerId)); // статусы медиатеки — всегда свежие, даже из кеша
  }

  $: saved = !!album?.deezerId && $savedAlbumIds.has(album.deezerId);

  // Вкладка: «bowling – сингл от rexv2 | kaikku».
  $: if (album) {
    const type = recordTypeLabel(album.type, $lang);
    setRouteTitle(`${$navStore.view}:${$navStore.id}`, $t('title.album', {
      title: album.title,
      type: $lang === 'ru' ? type.toLowerCase() : type,
      artists: album.artists.map((a) => a.name).join(', '),
    }));
  }
  $: totalSec = album?.durationSec || rows.reduce((s, r) => s + (r.durationMs || 0) / 1000, 0);

  // Играет ли сейчас этот альбом — включённый отсюда, из дискографии или с
  // главной (а не его трек из плейлиста или радио).
  $: playingHere = isContext($playingContext, 'album', $navStore.id);

  // Откуда играет — для истории прослушиваний.
  $: context = { kind: 'album', id: $navStore.id, title: album?.title }; // title — для «Из …» в очереди плеера

  // Скрытый релиз вне режима админа не играет (треки серые).
  $: unavailable = !!album?.hidden && !$canEdit;

  function onPlay() {
    if (unavailable) return;
    if (playingHere) return player.toggle();
    if (rows.length) playRows(rows, 0, context);
  }

  function moreMenu(e) {
    openMenu(e, [
      {
        label: $t('menu.addToQueue'),
        icon: ListEnd,
        onClick: () => {
          rows.forEach(queueRow);
          toast($t('menu.queued'));
        },
      },
      shareItem(album.href, album.title),
      ...($canEdit && album.deezerId && !album.own
        ? [
            { separator: true },
            {
              label: $t(album.hidden ? 'admin.showRelease' : 'admin.hideRelease'),
              icon: album.hidden ? Eye : EyeOff,
              onClick: toggleHidden,
            },
            { label: $t('admin.refreshRelease'), icon: RefreshCw, onClick: refreshFromDeezer },
          ]
        : []),
    ]);
  }

  async function refreshFromDeezer() {
    try {
      await api.refreshAlbum(album.deezerId);
      reload();
      toast($t('admin.releaseRefreshed'));
    } catch (e) {
      toast(e.message);
    }
  }

  async function toggleHidden() {
    try {
      const { hidden } = await api.setAlbumHidden(album.deezerId, !album.hidden);
      toast($t(hidden ? 'admin.releaseHidden' : 'admin.releaseShown'));
      reload();
    } catch (e) {
      toast(e.message);
    }
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if album}
  <div class="page-pad">
    <Hero image={album.coverUrl} kind={recordTypeLabel(album.type, $lang)} title={album.title} hidden={album.hidden} zoomLabel={$t('album.showCover')} on:zoom={() => (showCover = true)}>
      <svelte:fragment slot="meta">
        {#each album.artists as a, i (i)}
          {#if i > 0}<span class="dot">•</span>{/if}
          <a class="artist" href={a.href} use:link>
            {#if a.picture}<img class="avatar" src={sized(a.picture, 64)} alt="" />{:else}<span class="avatar ph"><UserRound size={14} /></span>{/if}
            <span>{a.name}</span>
          </a>
        {/each}
        {#if year(album.releaseDate)}<span class="dot">•</span><span>{year(album.releaseDate)}</span>{/if}
        <span class="dot">•</span><span class="muted-meta">{tracksCount(rows.length, $lang)}, {longDuration(totalSec, $lang)}</span>
      </svelte:fragment>
    </Hero>
  </div>

  {#if album.draft}
    <div class="draft-note page-pad">
      <span>{$t('upload.draftNote')}</span>
      {#if album.editable}<a href={paths.upload(album.deezerId)} use:link>{$t('upload.continue')}</a>{/if}
    </div>
  {/if}

  <div class="actions page-pad">
    <PlayButton size={56} playing={playingHere && $playerPlaying} disabled={unavailable} on:click={onPlay}
      label={playingHere && $playerPlaying ? $t('player.pause') : $t('player.play')} />
    <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
      <Shuffle size={28} />
    </button>
    {#if album.deezerId}
      <!-- Сохранить в свою медиатеку (главная); треки альбома скачиваются в фоне. -->
      <button class="icon-btn big save" class:saved title={$t(saved ? 'album.unsave' : 'album.save')} aria-label={$t(saved ? 'album.unsave' : 'album.save')} aria-pressed={saved} on:click={() => toggleSaveAlbum(album.deezerId)}>
        {#if saved}<CircleCheck size={32} />{:else}<CirclePlus size={32} />{/if}
      </button>
    {/if}
    {#if album.own}
      {#if album.editable}
        <a class="icon-btn" href={paths.upload(album.deezerId)} use:link title={$t('upload.editRelease')} aria-label={$t('upload.editRelease')}>
          <Pencil size={22} />
        </a>
      {/if}
    {:else if $canEdit && (album.libraryId || album.deezerId)}
      <button class="icon-btn" title={$t('edit.albumTitle')} aria-label={$t('edit.albumTitle')} on:click={() => (editing = true)}>
        <Pencil size={22} />
      </button>
    {/if}
    {#if rows.length}
      <button class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: album.title })} on:click={moreMenu}>
        <Ellipsis size={24} />
      </button>
    {/if}
  </div>

  <div class="page-pad">
    <TrackList {rows} {context} highlight={$navStore.track || null} on:changed={reload} />
    {#if longDate(album.releaseDate, $lang) || album.label}
      <footer class="release muted">
        {#if longDate(album.releaseDate, $lang)}<div>{longDate(album.releaseDate, $lang)}</div>{/if}
        {#if album.label}<div class="small">{$t('album.label', { label: album.label })}</div>{/if}
      </footer>
    {/if}
    {#if album.versions?.length}
      <AlbumVersions versions={album.versions} explicit={album.explicit} />
    {/if}
    <!-- Ещё альбомы каждого основного артиста (только для альбомов из Deezer). -->
    {#if album.deezerId}
      {#each album.artists.filter((a) => a.id) as a (a.id)}
        <MoreAlbums artist={a} excludeId={album.deezerId} />
      {/each}
    {/if}
  </div>

  {#if editing}
    {#await import('../components/EditDialog.svelte') then m}
      <svelte:component this={m.default} kind="album" id={album.libraryId} deezerId={album.deezerId} on:close={() => (editing = false)} on:saved={() => ((editing = false), reload())} />
    {/await}
  {/if}

  {#if showCover && album.coverUrl}
    <CoverLightbox src={album.coverUrl} alt={album.title} on:close={() => (showCover = false)} />
  {/if}
{:else if loading}
  <div class="page-pad skeleton">
    <div class="sk-art"></div>
    <div class="sk-lines"><div></div><div></div></div>
  </div>
{/if}

<style>
  .artist {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 700;
  }
  @media (hover: hover) {
    .artist:hover span {
      text-decoration: underline;
    }
  }
  .avatar {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
  }
  .avatar.ph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-dim);
  }
  .dot {
    margin: 0 4px;
  }
  .muted-meta {
    color: rgba(255, 255, 255, 0.7);
  }
  .draft-note {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 16px;
    margin-top: 16px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .draft-note span {
    padding: 4px 10px;
    border-radius: 999px;
    background: var(--bg-press);
    color: var(--text);
  }
  .draft-note a {
    font-weight: 700;
    color: var(--text);
    text-decoration: underline;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 24px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  .icon-btn.big {
    width: 44px;
    height: 44px;
  }
  .save.saved {
    color: var(--ok);
  }
  .small {
    font-size: 13px;
  }
  .release {
    padding: 32px 12px 16px;
    font-size: 14px;
  }
  .skeleton {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    padding-top: 48px;
  }
  .sk-art {
    width: 232px;
    height: 232px;
    border-radius: 6px;
    background: var(--bg-card);
  }
  .sk-lines div {
    height: 48px;
    width: 360px;
    max-width: 50vw;
    border-radius: 8px;
    background: var(--bg-card);
    margin-top: 12px;
  }
  @media (max-width: 768px) {
    .actions {
      gap: 16px;
    }
    .skeleton {
      flex-direction: column;
      align-items: center;
    }
  }
</style>
