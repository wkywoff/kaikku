<script>
  // Каркас: верхняя панель, прокручиваемая страница (экран — по адресу,
  // см. lib/route.js), плеер снизу. Плеер, очередь и полноэкранный режим
  // живут вне страниц и не прерываются при навигации.
  //
  // Цвета динамические: страница окрашивается в цвет своей обложки
  // (--page-*, stores/theme.js), плеер и акценты интерфейса — в цвет
  // обложки текущего трека (--player-*).
  import { onDestroy, onMount } from 'svelte';
  import { auth } from './lib/stores/auth.js';
  import { api } from './lib/api.js';
  import { navStore, scrollMemory, fromHistory } from './lib/stores/nav.js';
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

  // Ссылка на новый пароль (/?reset=…) открывает форму, даже если на этом
  // устройстве кто-то уже вошёл: старые входы после сброса всё равно не действуют.
  if (new URLSearchParams(location.search).has('reset')) auth.logout();

  // Вышли — сами или вход перестал действовать (сменили пароль, истёк):
  // плеер замолкает (он живёт вне страниц и играл бы дальше без кнопок), а
  // страница перезагружается — с ней уходит всё, что загружалось для этого
  // входа: главная, кеши страниц, медиатека.
  let wasAuthed = false;
  onDestroy(
    auth.subscribe((token) => {
      if (wasAuthed && !token) {
        player.pause();
        location.reload();
      }
      wasAuthed = !!token;
    }),
  );

  // Только вошёл/не вошёл: продление токена (api.js) не перезагружает данные.
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
      me = null; // 401 уже разлогинил в api.js
    }
    meStore.set(me);
  }

  // Палитра плеера — из обложки текущего трека; вешается на <html>, чтобы
  // её видели и топбар, и оверлеи.
  // Только при смене обложки: плеер меняется 4 раза в секунду (позиция), а
  // запись style у <html> заставляет браузер пересчитать стили всей страницы.
  let playerPalette = DEFAULT_PALETTE;
  $: cover = $nowPlaying?.coverUrl || null;
  $: loadPlayerPalette(cover);
  function loadPlayerPalette(url) {
    paletteForImage(url).then((p) => {
      if (url === cover) playerPalette = p;
    });
    // Большая обложка полноэкранного плеера — заранее в кеш браузера, чтобы
    // он открывался сразу, а не ждал картинку.
    if (url) setTimeout(() => (new Image().src = sized(url, 640)), 1500);
  }
  let rootVars = '';
  $: setRootVars(paletteVars('player', playerPalette));
  function setRootVars(v) {
    if (v !== rootVars) document.documentElement.style.cssText = rootVars = v;
  }

  // Подкраска шапки прокручивается вместе со страницей (background-attachment:
  // local), но под полосу прокрутки такой фон браузер не кладёт — там была
  // тёмная полоса. Под полосой — копия подкраски (main, ниже), сдвинутая на
  // прокрутку; дальше шапки двигать незачем.
  let tintScroll = 0;
  function onMainScroll(e) {
    const y = Math.min(e.currentTarget.scrollTop, 800);
    if (y !== tintScroll) tintScroll = y;
  }

  let queueOpen = false;
  let fullView = null; // null | 'overlay' | 'fullscreen'

  // Экраны, кроме главной и входа, и полноэкранный плеер — отдельными
  // кусками сборки: при открытии сайта браузер качает и разбирает только
  // главную. Остальное догружается в простое после первой отрисовки, так что
  // переходы и первое открытие полноэкранного плеера (8e57c4e) не ждут сети.
  // Итоги года и настройки — по требованию: редкие.
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
    settings: () => import('./lib/views/SettingsPage.svelte'),
    wrapped: () => import('./lib/views/WrappedPage.svelte'),
    uploads: () => import('./lib/views/UploadsPage.svelte'),
    upload: () => import('./lib/views/UploadPage.svelte'),
    missing: () => import('./lib/views/MissingPage.svelte'),
    user: () => import('./lib/views/UserPage.svelte'),
  };
  const RARE = ['settings', 'wrapped', 'uploads', 'upload', 'missing']; // загрузка — только у тех, кто загружает; «Не нашлось» — админ
  const BY_ID = ['playlist', 'mix', 'genre', 'artist', 'discography', 'album', 'upload', 'user']; // своя страница на каждый id
  let loaded = {}; // имя → компонент
  const loading = new Set();
  function need(name) {
    if (loaded[name] || loading.has(name) || !lazy[name]) return;
    loading.add(name);
    lazy[name]()
      .then((m) => (loaded = { ...loaded, [name]: m.default }))
      .catch((e) => console.error(`loading ${name}`, e))
      .finally(() => loading.delete(name));
  }
  $: need(view);
  $: if (queueOpen && hasTrack) need('queuePanel');
  $: if (fullView && hasTrack) need('nowPlaying');
  onMount(() => {
    const rest = () => Object.keys(lazy).filter((n) => !RARE.includes(n)).forEach(need);
    if ('requestIdleCallback' in window) requestIdleCallback(rest, { timeout: 4000 });
    else setTimeout(rest, 2000);
  });

  $: hasTrack = !!$nowPlaying;
  $: document.documentElement.lang = $lang;
  $: view = $navStore.view;

  // Заголовок вкладки, как в Spotify: пока играет — «Трек • Исполнители»,
  // иначе — страница: «kaikku – Любимые треки», «Альбом – сингл от
  // артиста | kaikku», «Артист | kaikku».
  $: routeKey = `${$navStore.view}:${$navStore.id || ''}`;
  // «Назад» жестом на iPhone показывает снимок страницы уже в её цвете —
  // плавная перекраска из цвета прежней страницы после него видна как
  // мигание. Вернулись по истории — цвет ставится сразу.
  $: instantTint = $navStore && fromHistory();
  const SCREEN_TITLES = { library: 'library.title', liked: 'liked.title', search: 'search.label', settings: 'settings.title', wrapped: 'wrapped.title', uploads: 'upload.title', missing: 'missing.title' };
  $: screenTitle =
    $routeTitle?.key === routeKey
      ? `${$routeTitle.text} | ${APP_NAME}`
      : SCREEN_TITLES[$navStore.view]
        ? `${APP_NAME} – ${$t(SCREEN_TITLES[$navStore.view])}`
        : APP_NAME;
  $: playingNow = $isPlaying ? $nowPlaying : null;
  $: document.title = playingNow ? `${playingNow.title} • ${playingNow.artist}` : screenTitle;
  // Chrome помнит заголовок у каждой записи истории и на «назад» (так же
  // закрывается плеер) показывает тот, что был у записи: открыли плеер на
  // одном треке, переключили, закрыли — во вкладке снова первый трек. Тот
  // же document.title заново браузер не замечает — ставим его через пустой.
  // Сразу в popstate: с таймером старое название успевало мелькнуть.
  function refreshTitle() {
    const title = document.title;
    document.title = '';
    document.title = title;
  }

  // Пробел — пауза/играть, где бы ни был фокус (как в Spotify): и на
  // ползунке громкости, и на кнопке, на которую только что нажали мышкой.
  // Кроме полей ввода текста и открытых окон: там пробел — это пробел.
  // Кнопка в фокусе пробелом не «нажимается»: браузеры (Firefox) жмут её
  // при отпускании клавиши, поэтому гасим и keyup.
  const TEXT_FIELDS = 'input:not([type=range]):not([type=checkbox]):not([type=radio]):not([type=button]):not([type=submit]), textarea, select';
  const isSpace = (e) => e.code === 'Space' || e.key === ' ';
  let spaceHeld = false;
  function onGlobalKey(e) {
    if (!isSpace(e)) return;
    if (e.ctrlKey || e.metaKey || e.altKey || !hasTrack) return;
    const el = e.target;
    if (el instanceof HTMLElement && (el.isContentEditable || el.closest(`${TEXT_FIELDS}, [aria-modal="true"]`))) return; // и в открытых окнах
    e.preventDefault();
    spaceHeld = true;
    if (!e.repeat) player.toggle();
  }
  function onGlobalKeyUp(e) {
    if (!isSpace(e) || !spaceHeld) return;
    spaceHeld = false;
    e.preventDefault();
  }

  // Переход на другую страницу закрывает меню трека и сворачивает
  // полноэкранный плеер (например, «Перейти к исполнителю» из его меню).
  $: $navStore, onNavigate();
  function onNavigate() {
    closeMenu();
    fullView = null;
  }
  // Вся страница в цвет обложки — только у альбомов и артистов (настройка).
  $: fullTint = $settings.pageTint === 'full' && ['album', 'artist'].includes(view);
</script>

<svelte:window on:keydown={onGlobalKey} on:keyup={onGlobalKeyUp} on:popstate={refreshTitle} />

{#if !authed}
  <Login />
{:else}
  <div class="shell" class:with-player={hasTrack}>
    <TopBar />
    <StatusBanner />

    <main use:scrollMemory style={paletteVars('page', $pagePalette)} style:--tint-scroll="{tintScroll}px" class:full-tint={fullTint} class:instant-tint={instantTint} on:scroll|passive={onMainScroll}>
      <div class="content">
        <!-- Режим админа переключили — страница пересоздаётся и грузится
             заново (скрытые релизы, lib/stores/me.js → pageEpoch). -->
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

    <PlayerBar
      {queueOpen}
      on:queue={() => (queueOpen = !queueOpen)}
      on:fullscreen={() => (fullView = 'fullscreen')}
      on:expand={() => (fullView = 'overlay')}
      on:lyrics={() => (lyricsOpen.set(true), (fullView = 'overlay'))}
    />
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
  main {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    /* Место под полосу прокрутки — всегда, как у Spotify: иначе на длинной
       странице колонка по центру сдвигалась на полполосы относительно
       короткой. */
    scrollbar-gutter: stable;
    margin: 0 8px;
    border-radius: var(--radius);
    isolation: isolate;
    /* Подкраска — фон самой прокручиваемой области (local: прокручивается
       вместе со страницей). Под полосу прокрутки local-фон не попадает —
       там её копия, сдвинутая на --tint-scroll (onMainScroll). */
    --hero-end: 330px;
    --tint-h: calc(var(--hero-end) + 232px);
    transition:
      --page-header 0.6s,
      --page-flat 0.6s;
    /* Шапка в цвет обложки, как у Spotify: сверху цвет во всю силу, к низу
       шапки (--hero-end) затемнён примерно на две трети, дальше уходит в
       фон. Затемнение идёт по одной плавной кривой (smoothstep): у
       градиента с изломом посередине на месте излома видна полоса. Смена
       цвета анимируется через @property (app.css). Затемнение — одна
       переменная на два места: страница и полоса прокрутки. */
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
    /* Цвет — на 2 px короче затемнения: у слоёв с общим нижним краем при
       дробной прокрутке (телефоны) край ложится между пикселями, оба слоя
       там полупрозрачны и цвет просвечивал тонкой линией поперёк страницы.
       Так его край закрыт непрозрачным концом затемнения. */
    background:
      var(--tint-fade) 0 0 / 100% var(--tint-h) no-repeat local,
      var(--tint-color) 0 0 / 100% calc(var(--tint-h) - 2px) no-repeat local,
      var(--tint-fade) right 0 top calc(-1 * var(--tint-scroll)) / 16px var(--tint-h) no-repeat scroll border-box,
      var(--tint-color) right 0 top calc(-1 * var(--tint-scroll)) / 16px calc(var(--tint-h) - 2px) no-repeat scroll border-box,
      var(--bg);
  }
  /* Открыто окно (lib/modal.js) — страница со своим окном поверх
     мини-плеера, нижней панели и полноэкранного плеера, но под меню «…» и
     уведомлениями. */
  :global(body.modal-open) main {
    z-index: 85;
  }
  /* На широком экране — колонкой по центру, как у Spotify (lib/layout.js):
     подкраска страницы остаётся во всю ширину, а текст и сетки не
     растягиваются на весь монитор. */
  .content {
    position: relative;
    min-height: 100%;
    max-width: 1955px;
    margin: 0 auto;
    padding-bottom: 32px;
  }
  /* Вся страница в цвет обложки, как у Apple Music: ровный глубокий цвет
     на всю высоту (--page-flat, color.js). Серые подложки карточек и строк —
     полупрозрачными, чтобы лежали на цвете. */
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
      --hero-end: 420px; /* на телефоне шапка выше: картинка над названием */
      /* Без полосы прокрутки, как в приложениях: иначе она появлялась, когда
         страница догружалась и становилась длиннее экрана, и мини-плеер
         сдвигался, чтобы на неё не заезжать. */
      scrollbar-width: none;
    }
    main::-webkit-scrollbar {
      display: none;
    }
    .shell main {
      margin-bottom: 0;
    }
    /* Низ страницы — над нижней панелью (BottomNav) и парящим над ней
       мини-плеером. */
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
