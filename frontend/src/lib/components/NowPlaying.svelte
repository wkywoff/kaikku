<script>
  // Полноэкранный плеер: огромная обложка, фон — несколько цветов обложки,
  // которые медленно переливаются, как у Apple Music (MeshBackground,
  // выключается в настройках), и все кнопки управления. На компьютере открывается кнопкой «во весь
  // экран» (и включает полноэкранный режим браузера), на телефоне — по
  // нажатию на мини-плеер. Закрывается Esc, кнопкой, свайпом вниз или
  // жестом «назад» (backCloses, stores/nav.js).
  import { sized } from '../imageproxy.js';
  import { swipeTrack } from '../swipe.js';
  import { onMount, createEventDispatcher } from 'svelte';
  import { fly } from 'svelte/transition';
  import { appleTouch } from '../platform.js';
  import { player, formatTime, isPlaying } from '../stores/player.js';
  import { settings } from '../stores/settings.js';
  import { link, backCloses } from '../stores/nav.js';
  import { marquee } from '../marquee.js';
  import { t } from '../i18n.js';
  import PlayButton from './PlayButton.svelte';
  import ArtistLinks from './ArtistLinks.svelte';
  import Slider from './Slider.svelte';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Minimize2 from '@lucide/svelte/icons/minimize-2';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import Repeat from '@lucide/svelte/icons/repeat';
  import Repeat1 from '@lucide/svelte/icons/repeat-1';
  import ListMusic from '@lucide/svelte/icons/list-music';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import EllipsisVertical from '@lucide/svelte/icons/ellipsis-vertical';
  import Moon from '@lucide/svelte/icons/moon';
  import { sleepMenuItems, sleepLabel } from '../sleepMenu.js';
  import LikeButton from './LikeButton.svelte';
  import MeshBackground from './MeshBackground.svelte';
  import LyricsView from './LyricsView.svelte';
  import QueueView from './QueueView.svelte';
  import { lyricsOpen } from '../stores/lyrics.js';
  import { openMenu, contextMenu } from '../stores/contextMenu.js';
  import { get } from 'svelte/store';
  import { trackMenuItems } from '../trackMenu.js';
  import { rowFromEntry } from '../library.js';
  import { focusTrap } from '../focusTrap.js';

  const smoothTime = player.smoothTime; // полоса прогресса плавная, цифры — по секундам

  export let browserFullscreen = false;

  const dispatch = createEventDispatcher();

  $: s = $player;
  // Соседние треки — их обложки лежат по бокам большой и видны, пока её
  // тянут пальцем (swipe.js): следующая въезжает на место, как в Spotify.
  // В маленькой обложке над текстом (телефон) соседние — вплотную.
  const PEEK_GAP = 24; // px, как .peek в стилях
  $: nextEntry =
    s.index + 1 < s.queue.length ? s.queue[s.index + 1] : s.repeat !== 'off' && s.queue.length > 1 ? s.queue[0] : null;
  $: prevEntry = s.index > 0 ? s.queue[s.index - 1] : s.repeat !== 'off' && s.queue.length > 1 ? s.queue[s.queue.length - 1] : null;
  $: track = s.queue[s.index];

  function close() {
    dispatch('close');
  }

  // Свайп от края экрана на iPhone — системный «назад»: запретить его сайт
  // не может, и он сам уводит плеер вбок за пальцем. Анимация закрытия
  // после него показала бы плеер ещё раз — закрываемся сразу.
  let closedByBack = false;
  const flyOut = (node) => (closedByBack && appleTouch ? { duration: 0 } : fly(node, { y: 40, duration: 220 }));

  // Текст и очередь — режимы плеера, как в Apple Music: справа от обложки на
  // компьютере, вместо неё на телефоне. Включён один из них; очередь не
  // запоминается, текст — да (stores/lyrics.js), и после очереди
  // возвращается.
  let showQueue = false;
  $: lyricsShown = $lyricsOpen && !showQueue;
  $: side = lyricsShown || showQueue;
  // Маленькая обложка над текстом (телефон) — назад к большой, как в Apple Music.
  function backToCover() {
    showQueue = false;
    lyricsOpen.set(false);
  }
  function toggleLyrics() {
    if (showQueue) {
      showQueue = false;
      lyricsOpen.set(true);
    } else lyricsOpen.update((on) => !on);
  }
  // «Назад» при открытой очереди закрывает её, а не плеер.
  let releaseQueueBack = null;
  $: syncQueueBack(showQueue);
  function syncQueueBack(on) {
    if (on && !releaseQueueBack) {
      releaseQueueBack = backCloses(() => {
        releaseQueueBack = null;
        showQueue = false;
      });
    } else if (!on && releaseQueueBack) {
      const release = releaseQueueBack;
      releaseQueueBack = null;
      release();
    }
  }

  // «…» рядом с лайком: меню трека и таймер сна.
  function openMore(e) {
    openMenu(e, [
      ...trackMenuItems(rowFromEntry(track)),
      { separator: true },
      { label: sleepLabel(s.sleep, $t), icon: Moon, submenu: sleepMenuItems().filter((i) => !i.heading) },
    ]);
  }

  // Свайп вниз закрывает плеер, как шторку в Apple Music/Spotify. Пока
  // плеер открыт, браузеру запрещено «потянуть, чтобы обновить» — иначе
  // тот же жест перезагружал страницу. Слайдеры и кнопки свайп не начинают;
  // если плеер прокручен (низкий экран), сначала прокручивается он.
  const CLOSE_AT = 110; // px
  // Плеер всё время на своём слое видеокарты (will-change в стилях): иначе
  // браузер выносил его туда в первый момент свайпа, перерисовывая всё с
  // большой обложкой, и начало свайпа дёргалось. Сдвиг — раз за кадр.
  let dragging = false; // пока тянут — фон не перерисовывается
  function swipeDown(node) {
    let start = null; // { x, y, t, axis }
    let dy = 0;
    let frame = 0;
    const apply = () => {
      frame = 0;
      node.style.transform = dy ? `translate3d(0, ${dy}px, 0)` : '';
    };
    const onStart = (e) => {
      if (e.touches.length !== 1 || node.querySelector('.now-playing')?.scrollTop > 0) return;
      // Текст песни листается пальцем — это не свайп плеера.
      if (e.target instanceof Element && e.target.closest('input, .slider, [role=slider], .lyrics, .queue-view .list')) return;
      start = { x: e.touches[0].clientX, y: e.touches[0].clientY, t: e.timeStamp, axis: null };
      dy = 0;
      node.style.transition = 'none';
    };
    // Направление решает первое движение: повели вбок (на iPhone — жест
    // «назад» от края, он сам уводит плеер) — вниз за пальцем плеер уже не
    // идёт, иначе он ехал бы сразу в две стороны.
    const AXIS_LOCK = 8; // px
    const onMove = (e) => {
      if (!start) return;
      const t = e.touches[0];
      if (!start.axis) {
        const ax = Math.abs(t.clientX - start.x);
        const ay = Math.abs(t.clientY - start.y);
        if (Math.max(ax, ay) < AXIS_LOCK) {
          if (t.clientY > start.y) e.preventDefault(); // и пока не решено — не прокрутка
          return;
        }
        start.axis = ax > ay ? 'x' : 'y';
      }
      if (start.axis === 'x') {
        start = null;
        dy = 0;
        dragging = false;
        cancelAnimationFrame(frame);
        frame = 0;
        node.style.transform = '';
        return;
      }
      dy = Math.max(0, t.clientY - start.y);
      if (dy > 0) {
        e.preventDefault(); // не прокрутка и не обновление страницы
        dragging = true;
      }
      if (!frame) frame = requestAnimationFrame(apply);
    };
    const onEnd = (e) => {
      if (!start) return;
      const fast = dy > 40 && dy / Math.max(1, e.timeStamp - start.t) > 0.6; // резкий короткий взмах
      start = null;
      dragging = false;
      cancelAnimationFrame(frame);
      frame = 0;
      node.style.transition = 'transform 0.22s ease-out';
      if (dy > CLOSE_AT || fast) {
        node.style.transform = 'translate3d(0, 100%, 0)';
        setTimeout(close, 200);
      } else {
        node.style.transform = '';
      }
    };
    node.addEventListener('touchstart', onStart, { passive: true });
    node.addEventListener('touchmove', onMove, { passive: false });
    node.addEventListener('touchend', onEnd);
    node.addEventListener('touchcancel', onEnd);
    const root = document.documentElement;
    const before = root.style.overscrollBehaviorY;
    root.style.overscrollBehaviorY = 'none';
    return {
      destroy() {
        node.removeEventListener('touchstart', onStart);
        node.removeEventListener('touchmove', onMove);
        node.removeEventListener('touchend', onEnd);
        node.removeEventListener('touchcancel', onEnd);
        root.style.overscrollBehaviorY = before;
      },
    };
  }

  // Текст на телефоне, как в Apple Music: пока песня играет и экран не
  // трогают 3 с, полоса с кнопками уезжает вниз и текст занимает весь
  // экран; любое касание возвращает её. На паузе и на компьютере (там
  // текст сбоку, а не над кнопками) не прячется.
  const DOCK_IDLE = 3000; // мс
  const narrow = matchMedia('(max-width: 900px)');
  let dock;
  let dockH = 0;
  let dockHidden = false;
  let dockTimer = 0;
  let swallowClick = false;
  function wake(e) {
    // Касание там, где была спрятанная полоса, только возвращает её — а
    // не перематывает на строку текста под пальцем.
    swallowClick = dockHidden && e?.type === 'pointerdown' && !!dock && e.clientY >= dock.getBoundingClientRect().top;
    dockHidden = false;
    clearTimeout(dockTimer);
    if (lyricsShown && $isPlaying && narrow.matches) {
      dockTimer = setTimeout(() => (dockHidden = !get(contextMenu)), DOCK_IDLE);
    }
  }
  $: (lyricsShown, $isPlaying, wake());
  function onClickCapture(e) {
    if (!swallowClick) return;
    swallowClick = false;
    e.stopPropagation();
    e.preventDefault();
  }

  onMount(() => {
    const closedByOther = backCloses(() => {
      closedByBack = true;
      close();
    });
    window.addEventListener('pointerdown', wake, true);
    window.addEventListener('keydown', wake, true);
    window.addEventListener('click', onClickCapture, true);
    if (browserFullscreen && document.fullscreenEnabled) {
      // Во весь экран — вся страница, а не только плеер: в полноэкранном
      // режиме браузер показывает лишь выбранный элемент, и меню «…» с
      // уведомлениями (они вне плеера) были бы не видны. Плеер и так
      // закрывает страницу целиком.
      document.documentElement.requestFullscreen().catch(() => {});
    }
    // Выход из полноэкранного режима браузера (Esc) закрывает и плеер.
    const onFsChange = () => {
      if (browserFullscreen && !document.fullscreenElement) close();
    };
    const onKey = (e) => {
      // Esc при открытом меню трека закрывает только меню.
      if (e.key === 'Escape' && !document.fullscreenElement && !get(contextMenu)) close();
    };
    document.addEventListener('fullscreenchange', onFsChange);
    window.addEventListener('keydown', onKey, true); // до обработчика меню, пока оно ещё открыто
    return () => {
      releaseQueueBack?.(); // очередь — запись поверх плеерной; обе снимаются разом
      closedByOther();
      clearTimeout(dockTimer);
      window.removeEventListener('pointerdown', wake, true);
      window.removeEventListener('keydown', wake, true);
      window.removeEventListener('click', onClickCapture, true);
      document.removeEventListener('fullscreenchange', onFsChange);
      window.removeEventListener('keydown', onKey, true);
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
      // На компьютере плеер снова открывается просто плеером: текст — по
      // кнопке. На телефоне он остаётся, как в Apple Music.
      if (!narrow.matches) lyricsOpen.set(false);
    };
  });
</script>

<!-- Фон — снаружи прокручиваемой части: внутри неё Safari на iPhone
     растягивал его только до отступов под «чёлкой» и полосой жестов. -->
<div class="np-shell" use:swipeDown use:focusTrap in:fly={{ y: 40, duration: 220 }} out:flyOut role="dialog" aria-label={$t('player.nowPlaying')}>
  <MeshBackground cover={track?.coverUrl || null} animated={$settings.animatedBg && !dragging && s.isPlaying} />
  <div class="now-playing" class:dock-hidden={dockHidden}>
    <!-- На компьютере — «свернуть» слева, на телефоне — «ручка» шторки по центру. -->
    <div class="top">
      <button class="icon-btn close" on:click={close} aria-label={$t('player.minimize')} title={$t('player.minimize')}>
        <span class="chev">{#if browserFullscreen}<Minimize2 size={22} />{:else}<ChevronDown size={26} />{/if}</span>
        <span class="grabber"></span>
      </button>
    </div>

    {#if track}
      <div class="body" class:with-side={side} class:queue-mode={showQueue} style="--dock-h: {dockH}px">
        <div class="art">
          <div
            class="art-slide"
            use:swipeTrack={{
              onNext: () => player.next(),
              onPrev: () => player.prev({ always: true }),
              canNext: player.canGoNext(s, $settings.autoplay),
              canPrev: !!prevEntry,
              slide: side && narrow.matches ? 0 : PEEK_GAP,
            }}
          >
            {#if prevEntry}
              <div class="peek prev" aria-hidden="true">
                {#if prevEntry.coverUrl}<img src={sized(prevEntry.coverUrl, 640)} alt="" decoding="async" />{:else}<div class="placeholder"><Disc3 size={96} /></div>{/if}
              </div>
            {/if}
            {#if track.coverUrl}<img src={sized(track.coverUrl, 640)} alt="" decoding="async" />{:else}<div class="placeholder"><Disc3 size={96} /></div>{/if}
            {#if nextEntry}
              <div class="peek next" aria-hidden="true">
                {#if nextEntry.coverUrl}<img src={sized(nextEntry.coverUrl, 640)} alt="" decoding="async" />{:else}<div class="placeholder"><Disc3 size={96} /></div>{/if}
              </div>
            {/if}
            {#if side}<button class="art-back" on:click={backToCover} aria-label={$t('player.backToCover')}></button>{/if}
          </div>
        </div>

        <div class="panel">
          <div class="title-row">
            <div class="meta">
              {#if track.albumHref}
                <a class="title" href={track.albumHref} use:link use:marquee={track.title} on:click={close} title={track.title}><span>{track.title}</span></a>
              {:else}
                <span class="title" use:marquee={track.title} title={track.title}><span>{track.title}</span></span>
              {/if}
              <span class="artist"><ArtistLinks menu artists={track.artists || [{ name: track.artist, href: track.artistHref }]} onNavigate={close} /></span>
            </div>
            <LikeButton deezerId={track.deezerId} size={28} />
            <button class="icon-btn more" class:sleeping={!!s.sleep} title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: track.title })} on:click={openMore}><EllipsisVertical size={18} /></button>
          </div>

          <div class="dock" bind:this={dock} bind:clientHeight={dockH}>
            <div class="progress">
              <Slider value={$smoothTime} max={s.duration || 0} step={0.1} label={$t('player.position')} on:change={(e) => player.seek(e.detail)} />
              <div class="times num"><span>{formatTime(s.currentTime)}</span><span>{formatTime(s.duration)}</span></div>
            </div>

            <div class="controls">
              <button class="icon-btn dup" class:on={s.shuffle} on:click={() => player.toggleShuffle()} title={$t('player.shuffle')} aria-pressed={s.shuffle}><Shuffle size={24} /></button>
              <button class="icon-btn big" on:click={() => player.prev()} aria-label={$t('player.prev')}>
                <svg viewBox="0 0 24 24" width="30" height="30" aria-hidden="true"><path d="M6 4.5v15M19 5.1v13.8a.8.8 0 0 1-1.25.66L8.6 12.66a.8.8 0 0 1 0-1.32l9.15-6.9A.8.8 0 0 1 19 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
              </button>
              <PlayButton variant="white" size={72} playing={s.isPlaying} loading={s.isLoading && !s.isPlaying} label={s.isPlaying ? $t('player.pause') : $t('player.play')} on:click={() => player.toggle()} />
              <button class="icon-btn big" on:click={() => player.next()} disabled={!player.canGoNext(s, $settings.autoplay)} aria-label={$t('player.next')}>
                <svg viewBox="0 0 24 24" width="30" height="30" aria-hidden="true"><path d="M18 4.5v15M5 5.1v13.8a.8.8 0 0 0 1.25.66l9.15-6.9a.8.8 0 0 0 0-1.32l-9.15-6.9A.8.8 0 0 0 5 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
              </button>
              <button class="icon-btn dup" class:on={s.repeat !== 'off'} on:click={() => player.cycleRepeat()} aria-pressed={s.repeat !== 'off'} title={$t(s.repeat === 'one' ? 'player.repeatOne' : s.repeat === 'all' ? 'player.repeatAll' : 'player.repeat')}>
                {#if s.repeat === 'one'}<Repeat1 size={24} />{:else}<Repeat size={24} />{/if}
              </button>
            </div>

            <!-- Текст и очередь — под «назад» и «вперёд», как в Apple Music. -->
            <div class="extras">
              <button class="icon-btn lyrics-btn" class:on={lyricsShown} on:click={toggleLyrics} aria-pressed={lyricsShown} aria-label={$t(lyricsShown ? 'lyrics.hide' : 'lyrics.show')} title={$t(lyricsShown ? 'lyrics.hide' : 'lyrics.show')}><svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
                  <!-- Залитый пузырь с вырезанными кавычками — значок текста Apple Music. -->
                  <mask id="np-lyrics-quotes"><rect width="24" height="24" fill="#fff" /><g fill="#000" stroke="#000" stroke-width="1.3" stroke-linecap="round"><circle cx="9" cy="9.6" r="1.6" stroke="none" /><circle cx="15" cy="9.6" r="1.6" stroke="none" /><path d="M10.5 9.8c0 1.9-1 3.1-2.6 3.7M16.5 9.8c0 1.9-1 3.1-2.6 3.7" fill="none" /></g></mask>
                  <path d="M6 3h12a3.5 3.5 0 0 1 3.5 3.5v8A3.5 3.5 0 0 1 18 18h-5.6l-4.6 3.5a.7.7 0 0 1-1.1-.56V18H6a3.5 3.5 0 0 1-3.5-3.5v-8A3.5 3.5 0 0 1 6 3z" fill="currentColor" mask="url(#np-lyrics-quotes)" />
                </svg></button>
              <button class="icon-btn queue-btn" class:on={showQueue} on:click={() => (showQueue = !showQueue)} aria-pressed={showQueue} aria-label={$t('player.queue')} title={$t('player.queue')}><ListMusic size={22} /></button>
            </div>
          </div>
        </div>

        {#if lyricsShown}
          <div class="side-col lyrics-col">
            {#key track.deezerId}<LyricsView deezerId={track.deezerId} />{/key}
          </div>
        {:else if showQueue}
          <div class="side-col queue-col"><QueueView /></div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .np-shell {
    position: fixed;
    inset: 0;
    z-index: 50;
    background: var(--player-dark);
    transition: background-color 1.2s;
    isolation: isolate;
    will-change: transform;
  }
  .now-playing {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    padding: 16px 24px max(24px, env(safe-area-inset-bottom));
    overflow-x: hidden; /* соседние обложки при свайпе (.peek) уходят за край */
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .top {
    display: flex;
    align-items: center;
  }
  .top .icon-btn {
    color: var(--text);
    width: 44px;
    height: 44px;
  }
  .grabber {
    display: none;
  }
  .body {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 64px;
    padding: 24px 0;
  }
  .art {
    width: min(56vh, 42vw);
    aspect-ratio: 1;
    flex-shrink: 0;
    border-radius: 8px;
    --art-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  /* Тянется пальцем (swipe.js) вместе с соседними обложками и тенью. */
  .art-slide {
    position: relative;
    height: 100%;
    border-radius: inherit;
    box-shadow: var(--art-shadow);
  }
  /* Соседние обложки — по бокам, видны только во время свайпа. */
  .peek {
    position: absolute;
    top: 0;
    width: 100%;
    height: 100%;
    border-radius: inherit;
    box-shadow: inherit;
    opacity: 0;
    pointer-events: none;
  }
  .peek.next {
    left: calc(100% + 24px);
  }
  .peek.prev {
    right: calc(100% + 24px);
  }
  .art-slide:global(.swiping) .peek {
    opacity: 1;
  }
  /* Кнопка поверх маленькой обложки — только на телефоне. */
  .art-back {
    display: none;
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: none;
    border: 0;
    padding: 0;
    cursor: pointer;
  }
  .art img,
  .placeholder {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
    display: block;
  }
  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .panel,
  .dock {
    display: flex;
    flex-direction: column;
    gap: 28px;
  }
  .panel {
    width: min(480px, 40vw);
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .title-row :global(.like) {
    width: 40px;
    height: 40px;
    flex-shrink: 0;
  }
  /* «…» — в полупрозрачном круге, как в Apple Music. */
  .more {
    position: relative;
    isolation: isolate; /* круг (::before) — под значком, а не под фоном плеера */
    flex-shrink: 0;
    width: 40px;
    height: 40px;
    color: var(--text);
  }
  .more::before {
    content: '';
    position: absolute;
    inset: 6px;
    z-index: -1;
    border-radius: 50%;
    background: rgba(255, 255, 255, 0.16);
  }
  @media (hover: hover) {
    .more:hover::before {
      background: rgba(255, 255, 255, 0.24);
    }
  }
  /* Таймер сна включён — точка на «…», он там. */
  .more.sleeping::after {
    content: '';
    position: absolute;
    top: 4px;
    right: 4px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
  }
  .meta {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  /* Название в одну строку; не влезло — бегущая строка (lib/marquee.js),
     строго в границах колонки. */
  .title {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    padding-bottom: 0.08em; /* под хвосты букв и подчёркивание */
    /* Точки Ё и кратка Й у Onest выше строки в 1.1 — без запаса сверху
       overflow их срезает; отрицательный margin оставляет вёрстку прежней. */
    padding-top: 0.08em;
    margin-top: -0.08em;
    font-size: clamp(24px, 3vw, 40px);
    font-weight: 800;
    letter-spacing: -0.03em;
    line-height: 1.1;
  }
  .title > span {
    display: inline-block;
  }
  .title:global(.marquee-on) {
    -webkit-mask-image: linear-gradient(90deg, transparent 0, #000 var(--marquee-fade), #000 calc(100% - 16px), transparent);
    mask-image: linear-gradient(90deg, transparent 0, #000 var(--marquee-fade), #000 calc(100% - 16px), transparent);
  }
  /* Исполнитель почти с название ростом, но тусклее — как в Apple Music. */
  .artist {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: clamp(18px, 1.9vw, 28px);
    line-height: 1.2;
    color: rgba(255, 255, 255, 0.62);
  }
  @media (hover: hover) {
    a.title:hover > span {
      text-decoration: underline; /* у inline-block подчёркивание родителя не видно */
    }
  }
  .times {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    margin-top: 4px;
  }
  /* Кнопки — на одной сетке с рядом ниже: текст и очередь стоят ровно под
     «назад» и «вперёд». */
  .controls,
  .extras {
    display: grid;
    grid-template-columns: 48px 1fr 48px 1fr 72px 1fr 48px 1fr 48px;
    align-items: center;
    justify-items: center;
  }
  .controls > :global(:nth-child(1)) {
    grid-column: 1;
  }
  .controls > :global(:nth-child(2)) {
    grid-column: 3;
  }
  .controls > :global(:nth-child(3)) {
    grid-column: 5;
  }
  .controls > :global(:nth-child(4)) {
    grid-column: 7;
  }
  .controls > :global(:nth-child(5)) {
    grid-column: 9;
  }
  .controls .icon-btn {
    color: rgba(255, 255, 255, 0.85);
    width: 48px;
    height: 48px;
  }
  .controls .icon-btn.on {
    color: var(--player-accent);
  }
  .icon-btn.big {
    color: var(--text);
  }
  .extras .icon-btn {
    width: 44px;
    height: 44px;
    border-radius: 10px;
    color: rgba(255, 255, 255, 0.6);
  }
  @media (hover: hover) {
    .extras .icon-btn:hover {
      color: var(--text);
    }
  }
  .lyrics-btn {
    grid-column: 3;
  }
  .queue-btn {
    grid-column: 7;
  }
  /* Текст или очередь включены — кнопка «залита», значок вырезан в ней,
     как в Apple Music. */
  .extras .icon-btn.on {
    background: rgba(255, 255, 255, 0.82);
    color: var(--player-dark);
  }

  /* С текстом или очередью, как в Apple Music: слева обложка и управление,
     справа текст (очередь) на всю высоту. */
  .body.with-side {
    display: grid;
    grid-template-columns: auto minmax(0, 680px);
    grid-template-rows: auto auto;
    align-content: center;
    justify-content: center;
    column-gap: clamp(32px, 6vw, 96px);
    row-gap: 24px;
    min-height: 0;
  }
  /* Под обложкой — название: длинная тень темнила бы его полосой. */
  .with-side .art {
    --art-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    width: min(42vh, 30vw);
    grid-column: 1;
    grid-row: 1;
    align-self: end;
  }
  .with-side .panel {
    width: min(42vh, 30vw);
    min-width: 300px;
    grid-column: 1;
    grid-row: 2;
    align-self: start;
  }
  /* Колонка с управлением тут уже — и название с исполнителем меньше. */
  .with-side .title {
    font-size: clamp(22px, 2.2vw, 30px);
  }
  .with-side .artist {
    font-size: clamp(17px, 1.5vw, 22px);
  }
  /* С очередью «перемешивать» и «повтор» — в её «таблетках»; здесь их
     место пустует, чтобы остальные кнопки не сдвигались. */
  .queue-mode .controls .dup {
    visibility: hidden;
  }
  .side-col {
    grid-column: 2;
    grid-row: 1 / 3;
    height: min(80vh, 820px);
    min-width: 0;
  }

  /* Телефон — раскладка Apple Music: сверху «ручка» шторки, обложка почти
     во всю ширину, под ней название с лайком и «…», полоса, кнопки и ряд
     «текст / очередь»; свободное место — вокруг кнопок. Низ — до самого
     края экрана, под полосой жестов (viewport-fit=cover в index.html). */
  @media (max-width: 900px) {
    .now-playing {
      padding: max(4px, env(safe-area-inset-top)) 24px calc(12px + env(safe-area-inset-bottom));
      /* Без полосы прокрутки, как в приложениях: раскладка рассчитана на
         экран, а если на каком-то телефоне не влезет на пару пикселей —
         пусть докручивается без полосы сбоку. */
      scrollbar-width: none;
    }
    .now-playing::-webkit-scrollbar {
      display: none;
    }
    .top {
      justify-content: center;
    }
    .top .close {
      width: 72px;
      height: 28px;
    }
    .chev {
      display: none;
    }
    .grabber {
      display: block;
      width: 40px;
      height: 5px;
      border-radius: 3px;
      background: rgba(255, 255, 255, 0.4);
    }
    .body {
      flex-direction: column;
      align-items: center;
      justify-content: flex-start;
      gap: 0;
      padding: 0;
      min-height: 0;
    }
    /* Размеры верхней части — переменными: с текстом (очередью) кнопки
       стоят ровно там же, где без него, и место под обложку и название
       там оставлено пустым (.body.with-side::before). */
    .body {
      --art-top: clamp(8px, 4svh, 40px);
      --title-top: clamp(16px, 5svh, 52px);
      --title-h: 58px;
      /* Обложка — сколько остаётся по высоте после остального (≈430 px
         с отступами), но не шире колонки. svh — видимая высота: во
         вкладке браузера адресная строка её отъедает, и с vh плеер
         вылезал за экран с полосой прокрутки. */
      --art-w: min(100vw - 48px, 520px, calc(100svh - 430px - env(safe-area-inset-top) - env(safe-area-inset-bottom)));
    }
    @supports not (height: 1svh) {
      .body {
        --art-top: clamp(8px, 4vh, 40px);
        --title-top: clamp(16px, 5vh, 52px);
        --art-w: min(100vw - 48px, 520px, calc(100vh - 430px));
      }
    }
    .art {
      width: var(--art-w);
      margin-top: var(--art-top);
      --art-shadow: 0 8px 24px rgba(0, 0, 0, 0.35); /* короткая: под ней название */
    }
    .panel {
      flex: 1;
      width: min(100%, 520px);
      gap: 0;
    }
    .title-row {
      height: var(--title-h);
      margin-top: var(--title-top);
      margin-right: -8px; /* «…» — по правому краю обложки, а не круга вокруг */
    }
    .title {
      font-size: 24px;
    }
    .artist {
      font-size: 21px;
    }
    .dock {
      flex: 1;
      gap: 0;
    }
    .progress {
      margin-top: clamp(12px, 2.5svh, 26px);
    }
    .controls {
      margin: auto 0;
      padding: 16px 0;
    }
    .extras {
      margin-bottom: clamp(0px, 2vh, 20px);
    }

    /* С текстом (очередью): сверху маленькая обложка с названием, лайком и
       «…», под ними текст, а полоса и кнопки — на тех же местах, что без
       текста: над ними пустое место размером с обложку и название. */
    /* overflow-y: clip — спрятанные кнопки уезжают вниз (translateY), и
       без обрезки этот сдвиг давал плееру лишнюю высоту: появлялась полоса
       прокрутки и его можно было листать. По горизонтали не режем — там
       поля строк текста. */
    .body.with-side {
      position: relative;
      display: flex; /* не сетка, как на компьютере */
      justify-content: flex-start;
      gap: 0;
      overflow-x: visible;
      overflow-y: clip;
      width: min(100%, 520px);
      margin: 0 auto;
    }
    .body.with-side::before {
      content: '';
      flex: none;
      height: calc(var(--art-top) + var(--art-w) + var(--title-top) + var(--title-h));
    }
    .with-side .art {
      position: absolute;
      top: 12px;
      left: 0;
      width: 60px;
      margin: 0;
      border-radius: 6px;
      box-shadow: 0 4px 16px rgba(0, 0, 0, 0.45);
    }
    /* Маленькая обложка — окошко: соседние при свайпе въезжают внутри
       него, вплотную, а не поверх названия справа. */
    .with-side .art {
      overflow: hidden;
    }
    .with-side .art-slide {
      box-shadow: none;
    }
    .with-side .peek.next {
      left: 100%;
    }
    .with-side .peek.prev {
      right: 100%;
    }
    .with-side .art-back {
      display: block;
    }
    .with-side .panel {
      display: contents;
    }
    .with-side .title-row {
      position: absolute;
      top: 12px;
      left: 74px;
      right: 0;
      height: 60px;
      margin-top: 0;
    }
    .with-side .title {
      font-size: 18px;
      font-weight: 700;
      letter-spacing: -0.02em;
    }
    .with-side .artist {
      font-size: 16px;
    }
    .with-side .meta {
      gap: 2px;
    }
    /* Текст — от шапки до самого низа, полоса с кнопками лежит поверх
       него; под ней текст растворяется (--lyrics-cut — высота полосы,
       LyricsView). Полоса спряталась — растворение уходит вниз, и текст
       виден целиком. Шире на поля строк (8 px): текст начинается ровно над
       обложкой. */
    .with-side .side-col {
      position: absolute;
      top: 84px;
      left: 0;
      right: 0;
      height: auto;
    }
    .with-side .lyrics-col {
      bottom: 0;
      left: -8px;
      right: -8px;
      --lyrics-cut: var(--dock-h);
      transition: --lyrics-cut 0.45s ease;
    }
    /* Очередь — между шапкой и кнопками, не под ними. */
    .with-side .queue-col {
      bottom: var(--dock-h);
      padding-top: 6px;
    }
    .with-side .dock {
      align-self: stretch;
      z-index: 1;
      transition:
        opacity 0.45s ease,
        transform 0.45s ease;
    }
    .dock-hidden .with-side .lyrics-col {
      --lyrics-cut: 0px;
    }
    .dock-hidden .with-side .dock {
      opacity: 0;
      transform: translateY(24px);
      visibility: hidden;
      transition:
        opacity 0.45s ease,
        transform 0.45s ease,
        visibility 0s 0.45s;
    }
  }
</style>
