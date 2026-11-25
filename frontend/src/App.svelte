<script>
  import { onDestroy, onMount } from 'svelte';
  import { auth } from './lib/stores/auth.js';
  import { clearAccountData } from './lib/signout.js';
  import { api } from './lib/api.js';
  import { navStore, scrollMemory, fromHistory, reopensOnForward } from './lib/stores/nav.js';
  import { player, nowPlaying, isPlaying } from './lib/stores/player.js';
  import { pagePalette } from './lib/stores/theme.js';
  import { lyricsOpen } from './lib/stores/lyrics.js';
  import { paletteForImage, paletteVars, DEFAULT_PALETTE } from './lib/color.js';
  import { sized } from './lib/imageproxy.js';
  import Login from './lib/views/Login.svelte';
  import Home from './lib/views/Home.svelte';
  import { loadCollection } from './lib/stores/collection.js';
  import { closeMenu } from './lib/stores/contextMenu.js';
  import ContextMenu from './lib/components/ContextMenu.svelte';
  import Toast from './lib/components/Toast.svelte';
  import StatusBanner from './lib/components/StatusBanner.svelte';
  import { lang, settings } from './lib/stores/settings.js';
  import { me as meStore, pageEpoch } from './lib/stores/me.js';
  import { routeTitle, APP_NAME } from './lib/stores/title.js';
  import { t } from './lib/i18n.js';
  import TopBar from './lib/components/TopBar.svelte';
  import BottomNav from './lib/components/BottomNav.svelte';
  import PlayerBar from './lib/components/PlayerBar.svelte';
  import { sidePlayer, sideFits, clampWidth } from './lib/stores/sidePlayer.js';

  if (new URLSearchParams(location.search).has('reset')) {
    clearAccountData();
    auth.logout();
  }

  let wasAuthed = false;
  onDestroy(
    auth.subscribe((token) => {
      if (wasAuthed && !token) {
        clearAccountData();
        location.reload();
      }
      wasAuthed = !!token;
    }),
  );

  $: authed = !!$auth;

  let me = null;
  $: if (authed) {
    loadMe();
    loadCollection();
  }
  else {
    me = null;
    meStore.set(null);
  }

  async function loadMe() {
    try {
      me = await api.me();
    } catch {
      me = null;
    }
    meStore.set(me);
  }

  let playerPalette = DEFAULT_PALETTE;
  $: cover = $nowPlaying?.coverUrl || null;
  $: loadPlayerPalette(cover);
  function loadPlayerPalette(url) {
    paletteForImage(url).then((p) => {
      if (url === cover) playerPalette = p;
    });
    if (url) setTimeout(() => (new Image().src = sized(url, 640)), 1500);
  }
  let rootVars = '';
  $: setRootVars(paletteVars('player', playerPalette));
  function setRootVars(v) {
    if (v !== rootVars) document.documentElement.style.cssText = rootVars = v;
  }

  let tintScroll = 0;
  function onMainScroll(e) {
    const y = Math.min(e.currentTarget.scrollTop, 800);
    if (y !== tintScroll) tintScroll = y;
  }

  let queueOpen = false;
  let fullView = null;
  onDestroy(
    reopensOnForward('player', () => {
      if (hasTrack) fullView = 'overlay';
      return hasTrack;
    }),
  );

  const lazy = {
    library: () => import('./lib/views/LibraryPage.svelte'),
    search: () => import('./lib/views/SearchPage.svelte'),
    liked: () => import('./lib/views/LikedPage.svelte'),
    playlist: () => import('./lib/views/PlaylistPage.svelte'),
    mix: () => import('./lib/views/MixPage.svelte'),
    genre: () => import('./lib/views/GenrePage.svelte'),
    artist: () => import('./lib/views/ArtistPage.svelte'),
    discography: () => import('./lib/views/DiscographyPage.svelte'),
    album: () => import('./lib/views/AlbumPage.svelte'),
    nowPlaying: () => import('./lib/components/NowPlaying.svelte'),
    queuePanel: () => import('./lib/components/QueuePanel.svelte'),
    sidePlayer: () => import('./lib/components/SidePlayer.svelte'),
    settings: () => import('./lib/views/SettingsPage.svelte'),
    wrapped: () => import('./lib/views/WrappedPage.svelte'),
    wrappedAlbums: () => import('./lib/views/WrappedAlbumsPage.svelte'),
    uploads: () => import('./lib/views/UploadsPage.svelte'),
    releaseEdit: () => import('./lib/views/ReleaseEditPage.svelte'),
    missing: () => import('./lib/views/MissingPage.svelte'),
    user: () => import('./lib/views/UserPage.svelte'),
  };
  const RARE = ['settings', 'wrapped', 'wrappedAlbums', 'uploads', 'releaseEdit', 'missing'];
  const BY_ID = ['playlist', 'mix', 'genre', 'artist', 'discography', 'album', 'releaseEdit', 'user'];
  let loaded = {};
  const loading = new Set();
  function need(name, onFail) {
    if (loaded[name] || loading.has(name) || !lazy[name]) return;
    loading.add(name);
    lazy[name]()
      .then((m) => (loaded = { ...loaded, [name]: m.default }))
      .catch((e) => {
        console.error(`loading ${name}`, e);
        onFail?.();
      })
      .finally(() => loading.delete(name));
  }
  $: need(view);
  $: if (queueOpen && hasTrack) need('queuePanel');
  $: if (fullView && hasTrack) need('nowPlaying');
  const SIDE_LATE_MS = 3000;
  let sideLate = false;
  let sideTimer = 0;
  $: sideWanted = $sidePlayer.open && $sideFits && hasTrack;
  $: sideReady = !!loaded.sidePlayer;
  $: awaitSide(sideWanted && !sideReady, $sidePlayer);
  function awaitSide(waiting) {
    clearTimeout(sideTimer);
    sideLate = false;
    if (!waiting) return;
    need('sidePlayer', () => (sideLate = true));
    sideTimer = setTimeout(() => (sideLate = true), SIDE_LATE_MS);
  }
  $: sideShown = sideWanted && (sideReady || !sideLate);
  $: if (sideShown && queueOpen) queueOpen = false;
  onMount(() => {
    const rest = () => Object.keys(lazy).filter((n) => !RARE.includes(n)).forEach((n) => need(n));
    if ('requestIdleCallback' in window) requestIdleCallback(rest, { timeout: 4000 });
    else setTimeout(rest, 2000);
  });

  $: hasTrack = !!$nowPlaying;
  $: document.documentElement.lang = $lang;
  $: view = $navStore.view;

  $: routeKey = `${$navStore.view}:${$navStore.id || ''}`;
  $: instantTint = $navStore && fromHistory();
  const SCREEN_TITLES = { library: 'library.title', liked: 'liked.title', search: 'search.label', settings: 'settings.title', wrapped: 'wrapped.title', wrappedAlbums: 'wrapped.albumsTitle', uploads: 'upload.title', missing: 'missing.title' };
  $: screenTitle =
    $routeTitle?.key === routeKey
      ? `${$routeTitle.text} | ${APP_NAME}`
      : SCREEN_TITLES[$navStore.view]
        ? `${APP_NAME} – ${$t(SCREEN_TITLES[$navStore.view])}`
        : APP_NAME;
  $: playingNow = $isPlaying ? $nowPlaying : null;
  $: document.title = playingNow ? `${playingNow.title} • ${playingNow.artist}` : screenTitle;
  function refreshTitle() {
    const title = document.title;
    document.title = '';
    document.title = title;
  }

  const TEXT_FIELDS = 'input:not([type=range]):not([type=checkbox]):not([type=radio]):not([type=button]):not([type=submit]), textarea, select';
  const isSpace = (e) => e.code === 'Space' || e.key === ' ';
  let spaceHeld = false;
  function onGlobalKey(e) {
    if (!isSpace(e)) return;
    if (e.ctrlKey || e.metaKey || e.altKey || !hasTrack) return;
    const el = e.target;
    if (el instanceof HTMLElement && (el.isContentEditable || el.closest(`${TEXT_FIELDS}, [aria-modal="true"]`))) return;
    e.preventDefault();
    spaceHeld = true;
    if (!e.repeat) player.toggle();
  }
  function onGlobalKeyUp(e) {
    if (!isSpace(e) || !spaceHeld) return;
    spaceHeld = false;
    e.preventDefault();
  }

  $: $navStore, onNavigate();
  function onNavigate() {
    closeMenu();
    fullView = null;
  }
  $: fullTint = $settings.pageTint === 'full' && ['album', 'artist'].includes(view);
</script>

<svelte:window on:keydown={onGlobalKey} on:keyup={onGlobalKeyUp} on:popstate={refreshTitle} />

{#if !authed}
  <Login />
{:else}
  <div class="shell" class:with-player={hasTrack}>
    <TopBar />
    <StatusBanner />

    <div class="workspace" class:with-side={sideShown}>
      <main use:scrollMemory style={paletteVars('page', $pagePalette)} style:--tint-scroll="{tintScroll}px" class:full-tint={fullTint} class:instant-tint={instantTint} on:scroll|passive={onMainScroll}>
        <div class="content">
          {#key $pageEpoch}
          {#if view === 'home'}
            <Home />
          {:else if loaded[view] && view === 'settings'}
            <svelte:component this={loaded.settings} {me} />
          {:else if loaded[view] && BY_ID.includes(view)}
            {#key $navStore.id}<svelte:component this={loaded[view]} />{/key}
          {:else if loaded[view]}
            <svelte:component this={loaded[view]} />
          {/if}
          {/key}
        </div>
      </main>
      {#if sideShown}
        {#if sideReady}
          <svelte:component
            this={loaded.sidePlayer}
            on:fullscreen={() => (fullView = 'fullscreen')}
            on:lyrics={() => (lyricsOpen.set(true), (fullView = 'overlay'))}
          />
        {:else}
          <div class="side-holder" class:tint-full={$settings.playerTint === 'full'} style="width: {clampWidth($sidePlayer.width, window.innerWidth)}px" aria-hidden="true"></div>
        {/if}
      {/if}
    </div>

    {#if !sideShown}
      <PlayerBar
        {queueOpen}
        on:queue={() => (queueOpen = !queueOpen)}
        on:fullscreen={() => (fullView = 'fullscreen')}
        on:expand={() => (fullView = 'overlay')}
        on:lyrics={() => (lyricsOpen.set(true), (fullView = 'overlay'))}
      />
    {/if}
    <BottomNav />
  </div>

  {#if queueOpen && hasTrack && loaded.queuePanel}
    <svelte:component this={loaded.queuePanel} on:close={() => (queueOpen = false)} />
  {/if}

  <ContextMenu />
  <Toast />

  {#if fullView && hasTrack && loaded.nowPlaying}
    <svelte:component
      this={loaded.nowPlaying}
      browserFullscreen={fullView === 'fullscreen'}
      on:close={() => (fullView = null)}
    />
  {/if}
{/if}

<style>
  .shell {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #000;
  }
  .workspace {
    flex: 1;
    min-height: 0;
    display: flex;
  }
  .workspace.with-side {
    padding-bottom: 8px;
  }
  .workspace.with-side main {
    margin-right: 0;
  }
  .side-holder {
    flex-shrink: 0;
    margin: 0 8px;
    border-radius: var(--radius);
    background: var(--bg);
  }
  .side-holder.tint-full {
    background: var(--player-flat);
  }
  main {
    position: relative;
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    scrollbar-gutter: stable;
    margin: 0 8px;
    border-radius: var(--radius);
    isolation: isolate;
    --hero-end: 330px;
    --tint-h: calc(var(--hero-end) + 232px);
    transition:
      --page-header 0.6s,
      --page-flat 0.6s;
    --tint-fade: linear-gradient(
          180deg,
          color-mix(in srgb, var(--bg) 0%, transparent) 0%,
          color-mix(in srgb, var(--bg) 2.8%, transparent) 10%,
          color-mix(in srgb, var(--bg) 10.4%, transparent) 20%,
          color-mix(in srgb, var(--bg) 21.6%, transparent) 30%,
          color-mix(in srgb, var(--bg) 35.2%, transparent) 40%,
          color-mix(in srgb, var(--bg) 50%, transparent) 50%,
          color-mix(in srgb, var(--bg) 64.8%, transparent) 60%,
          color-mix(in srgb, var(--bg) 78.4%, transparent) 70%,
          color-mix(in srgb, var(--bg) 89.6%, transparent) 80%,
          color-mix(in srgb, var(--bg) 97.2%, transparent) 90%,
          color-mix(in srgb, var(--bg) 100%, transparent) 100%
        );
    --tint-color: linear-gradient(var(--page-header), var(--page-header));
    background:
      var(--tint-fade) 0 0 / 100% var(--tint-h) no-repeat local,
      var(--tint-color) 0 0 / 100% calc(var(--tint-h) - 2px) no-repeat local,
      var(--tint-fade) right 0 top calc(-1 * var(--tint-scroll)) / 16px var(--tint-h) no-repeat scroll border-box,
      var(--tint-color) right 0 top calc(-1 * var(--tint-scroll)) / 16px calc(var(--tint-h) - 2px) no-repeat scroll border-box,
      var(--bg);
  }
  :global(body.modal-open) main {
    z-index: 85;
  }
  @media not all and (max-width: 768px) {
    :global(body.modal-keep-player) main {
      z-index: 85;
    }
  }
  .content {
    position: relative;
    min-height: 100%;
    max-width: 1955px;
    margin: 0 auto;
    padding-bottom: 32px;
  }
  main.instant-tint {
    transition: none;
  }
  main.full-tint {
    --bg-card: rgba(255, 255, 255, 0.07);
    --bg-press: rgba(255, 255, 255, 0.12);
    background: var(--page-flat);
  }
  .content :global(.page-pad) {
    padding-left: 32px;
    padding-right: 32px;
  }

  @media (max-width: 768px) {
    main {
      margin: 0;
      border-radius: 0;
      --hero-end: 420px;
      scrollbar-width: none;
    }
    main::-webkit-scrollbar {
      display: none;
    }
    .shell main {
      margin-bottom: 0;
    }
    .content {
      padding-bottom: calc(var(--tabbar-h) + 24px + env(safe-area-inset-bottom));
    }
    .shell.with-player .content {
      padding-bottom: calc(var(--tabbar-h) + 88px + env(safe-area-inset-bottom));
    }
    .content :global(.page-pad) {
      padding-left: 16px;
      padding-right: 16px;
    }
  }
</style>
