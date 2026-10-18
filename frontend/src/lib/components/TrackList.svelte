<script>
  // Список треков в стиле Spotify. Два вида (настройка «Вид треклистов»):
  //   list    — обложка, название, исполнители под названием;
  //   compact — одна строка: # | название | исполнители | длительность.
  // Номер при наведении превращается в Play, у играющего — эквалайзер.
  // Справа — «в любимые» (плюс; галочка, если уже там) и «…» с меню; то же
  // меню — по правому клику на строке. Как в Spotify: на компьютере клик
  // выделяет строку, двойной клик (или Play вместо номера, или Enter)
  // играет трек; на телефоне — одно касание. Нескачанное играет потоком и
  // скачивается в фоне (lib/library.js).
  //
  // reorderable — свой плейлист: строки перетаскиваются (мышью — за любое
  // место строки, на сенсорном экране — за ручку справа), событие reorder
  // { from, to } — новые индексы. highlight — Deezer ID трека, который
  // выделить и показать (ссылка «Поделиться» на трек).
  import { isLocalId } from '../links.js';
  import { sized } from '../imageproxy.js';
  import { longpress } from '../longpress.js';
  import { player, formatTime, nowPlaying, isPlaying as playerPlaying } from '../stores/player.js';
  import { link, fromHistory, paths } from '../stores/nav.js';
  import { settings } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { libraryState, playRows, playWithSimilar, trackIdOf, isCurrentRow } from '../library.js';
  import { likedIds, toggleLike } from '../stores/collection.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { trackMenuItems } from '../trackMenu.js';
  import ArtistLinks from './ArtistLinks.svelte';
  import { createEventDispatcher, tick, onDestroy } from 'svelte';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CirclePlus from '@lucide/svelte/icons/circle-plus';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Clock3 from '@lucide/svelte/icons/clock-3';
  import GripVertical from '@lucide/svelte/icons/grip-vertical';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import { canEdit } from '../stores/me.js';

  export let rows = [];
  const restored = fromHistory(); // открыли «назад»/«вперёд»: обложки уже в кеше (MediaCard)
  export let showCover = false; // только для вида list
  export let showAlbum = false;
  export let numbered = true; // false — без номеров (поиск)
  export let header = true;
  export let view = null; // null — из настроек
  export let playlist = null; // { id, mine } — список и есть этот плейлист (меню «убрать из плейлиста»)
  export let context = null; // откуда играет, для истории: { kind, id } (lib/library.js → playRows)
  export let reorderable = false;
  export let highlight = null;
  export let playSimilar = false; // поиск: после трека — похожие, а не следующая строка
  export let showAddedBy = false; // совместный плейлист: «добавил X» под исполнителями

  $: mode = view || $settings.trackView;
  $: compact = mode === 'compact';

  // Заголовки «Диск N» — только если в списке больше одного диска.
  $: multiDisc = new Set(rows.map((r) => r.discNo || 1)).size > 1;

  // changed — страница перезагружает данные (после правки админа);
  // remove — убрать строку из плейлиста (страница плейлиста).
  const dispatch = createEventDispatcher();
  let editingTrack = null; // Deezer ID трека, который правит админ

  $: current = $nowPlaying;
  let selected = null; // выделенная строка (компьютер)
  let listEl;

  // Трек скрытого релиза (админ убрал релиз): вне режима админа — серый и не
  // играет, как недоступные треки у Spotify.
  const unavailable = (row) => row.hidden && !$canEdit;

  function play(i) {
    if (unavailable(rows[i])) return;
    if (isCurrentRow(rows[i], current, $libraryState)) player.toggle();
    else if (playSimilar) playWithSimilar(rows[i]);
    else playRows(rows, i, context);
  }

  // Клик по ссылке или кнопке внутри строки — их собственное действие, не
  // строки. Путь события, а не e.target.closest: кнопка лайка успевает
  // поменять иконку до того, как клик дойдёт до строки, и старая иконка к
  // этому моменту уже не в документе.
  const fromControl = (e) => e.composedPath().some((el) => el instanceof Element && el.matches('a, button, .grip'));
  const tapToPlay = () => matchMedia('(hover: none)').matches; // сенсорный экран — играть с одного касания

  function onRowClick(e, i) {
    if (justDragged) {
      justDragged = false;
      return;
    }
    if (fromControl(e)) return;
    if (tapToPlay()) play(i);
    else selected = i;
  }

  function onRowDblClick(e, i) {
    if (!fromControl(e)) play(i);
  }

  function onKey(e, i) {
    if (e.key === 'Enter') {
      e.preventDefault();
      play(i);
    }
  }

  // Клик мимо списка снимает выделение.
  function onWindowPointer(e) {
    if (selected !== null && listEl && !listEl.contains(e.target)) selected = null;
  }

  // --- выделить трек по ссылке ---
  let highlighted = null;
  $: if (highlight && highlight !== highlighted && rows.length) showRow(highlight);
  async function showRow(id) {
    const i = rows.findIndex((r) => r.deezerId === id);
    if (i < 0) return;
    highlighted = id;
    selected = i;
    await tick();
    listEl?.querySelectorAll('.row')[i]?.scrollIntoView({ block: 'center' });
  }

  // --- перетаскивание (свой плейлист) ---
  // drag: { from, to, y, active } — to: «щель» перед строкой to (0…rows.length).
  let drag = null;
  let justDragged = false; // клик после перетаскивания — не выделение строки
  let scrollFrame = 0;
  const DRAG_THRESHOLD = 6;
  const EDGE = 64; // у края экрана список сам прокручивается

  function onPointerDown(e, i) {
    if (!reorderable || e.button !== 0) return;
    const handle = e.target instanceof Element && e.target.closest('.grip');
    if (!handle && (e.pointerType !== 'mouse' || fromControl(e))) return;
    drag = { from: i, to: i, y: e.clientY, startY: e.clientY, active: !!handle, pointerId: e.pointerId };
    if (handle) scrollFrame = requestAnimationFrame(autoScroll);
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', stopDrag);
  }

  function onPointerMove(e) {
    if (!drag || e.pointerId !== drag.pointerId) return;
    if (!drag.active) {
      if (Math.abs(e.clientY - drag.startY) < DRAG_THRESHOLD) return;
      drag.active = true;
      scrollFrame = requestAnimationFrame(autoScroll);
    }
    e.preventDefault();
    drag = { ...drag, y: e.clientY, to: gapAt(e.clientY) };
  }

  function gapAt(y) {
    const els = listEl.querySelectorAll('.row');
    for (let i = 0; i < els.length; i++) {
      const r = els[i].getBoundingClientRect();
      if (y < r.top + r.height / 2) return i;
    }
    return els.length;
  }

  function autoScroll() {
    if (!drag?.active) return;
    const scroller = listEl.closest('main');
    if (scroller) {
      const box = scroller.getBoundingClientRect();
      const dy = drag.y < box.top + EDGE ? -12 : drag.y > box.bottom - EDGE ? 12 : 0;
      if (dy) {
        scroller.scrollTop += dy;
        drag = { ...drag, to: gapAt(drag.y) };
      }
    }
    scrollFrame = requestAnimationFrame(autoScroll);
  }

  function onPointerUp(e) {
    if (!drag || e.pointerId !== drag.pointerId) return;
    const { from, to, active } = drag;
    stopDrag();
    if (!active) return;
    justDragged = e.pointerType === 'mouse'; // мышь после отпускания ещё «кликнет» по строке
    const target = to > from ? to - 1 : to;
    if (target !== from) {
      selected = target;
      dispatch('reorder', { from, to: target });
    }
  }

  function stopDrag() {
    drag = null;
    cancelAnimationFrame(scrollFrame);
    window.removeEventListener('pointermove', onPointerMove);
    window.removeEventListener('pointerup', onPointerUp);
    window.removeEventListener('pointercancel', stopDrag);
  }
  onDestroy(stopDrag);

  function menu(e, row) {
    openMenu(
      e,
      trackMenuItems(row, {
        playlist,
        onRemove: () => dispatch('remove', row),
        // Свои треки (загрузка, k…) правятся в окне загрузки, не здесь.
        onEdit: row.deezerId && !isLocalId(row.deezerId) ? () => (editingTrack = row.deezerId) : null,
      }),
    );
  }
</script>

<svelte:window on:pointerdown={onWindowPointer} />

<div class="tracklist" class:compact class:with-album={showAlbum} class:reorderable class:dragging={drag?.active} class:album-numbers={numbered && !showCover} bind:this={listEl}>
  {#if header}
    <div class="head">
      <span class="c-num">{numbered ? '#' : ''}</span>
      <span>{$t('tracks.title')}</span>
      {#if compact}<span class="c-artist">{$t('tracks.artist')}</span>{/if}
      {#if showAlbum}<span class="c-album">{$t('tracks.album')}</span>{/if}
      <span></span>
      <span class="c-dur" title={$t('tracks.duration')}><Clock3 size={16} /></span>
      <span></span>
    </div>
  {/if}

  {#each rows as row, i (row.itemId || row.deezerId || row.libraryTrackId || i)}
    {@const playing = isCurrentRow(row, current, $libraryState)}
    {@const liked = !!row.deezerId && $likedIds.has(row.deezerId)}
    {#if multiDisc && (i === 0 || (rows[i - 1].discNo || 1) !== (row.discNo || 1))}
      <div class="disc">{$t('tracks.disc', { n: row.discNo || 1 })}</div>
    {/if}
    <div
      class="row"
      class:playing
      class:unavailable={row.hidden && !$canEdit}
      class:selected={selected === i}
      class:lifted={drag?.active && drag.from === i}
      class:drop-before={drag?.active && drag.to === i && drag.to !== drag.from && drag.to !== drag.from + 1}
      class:drop-after={drag?.active && i === rows.length - 1 && drag.to === rows.length && drag.from !== rows.length - 1}
      role="button"
      tabindex="0"
      aria-label={row.title}
      on:click={(e) => onRowClick(e, i)}
      on:dblclick={(e) => onRowDblClick(e, i)}
      on:keydown={(e) => onKey(e, i)}
      on:contextmenu={(e) => menu(e, row)}
      use:longpress={(e) => menu(e, row)}
      on:pointerdown={(e) => onPointerDown(e, i)}
    >
      <span class="c-num">
        <!-- Номер; при наведении — Play (играет с одного клика). -->
        <button class="num-btn" tabindex="-1" aria-label={playing && $playerPlaying ? $t('player.pause') : $t('player.play')} on:click={() => play(i)}>
          {#if playing && $playerPlaying}
            <span class="eq" aria-label={$t('tracks.playing')}><i></i><i></i><i></i></span>
          {:else}
            <span class="n num">{numbered ? row.trackNo || i + 1 : ''}</span>
            <svg class="hover-play" viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <path d="M7 3.9v16.2a1 1 0 0 0 1.52.85l13.06-8.1a1 1 0 0 0 0-1.7L8.52 3.05A1 1 0 0 0 7 3.9z" fill="currentColor" />
            </svg>
          {/if}
        </button>
      </span>

      <span class="c-title">
        {#if showCover && !compact}
          {#if row.coverUrl}<img class="cover" src={sized(row.coverUrl, 64)} alt="" loading={restored ? 'eager' : 'lazy'} draggable="false" />{:else}<span class="cover"></span>{/if}
        {/if}
        <span class="text">
          <span class="title-line">
            <span class="title">{row.title}</span>
            {#if row.explicit}<span class="explicit" title={$t('tracks.explicit')}>E</span>{/if}
            {#if row.hidden && $canEdit}<span class="hidden-mark" title={$t('admin.hiddenRelease')}><EyeOff size={14} /></span>{/if}
          </span>
          <span class="artists"><ArtistLinks artists={row.artists} /></span>
          {#if showAddedBy && row.addedBy}<a class="added-by" href={paths.user(row.addedBy)} use:link>{$t('playlist.addedBy', { name: row.addedBy })}</a>{/if}
        </span>
      </span>

      {#if compact}
        <span class="c-artist"><ArtistLinks artists={row.artists} /></span>
      {/if}

      {#if showAlbum}
        <span class="c-album">
          {#if row.album?.href}<a href={row.album.href} use:link>{row.album.title}</a>{:else}{row.album?.title ?? ''}{/if}
        </span>
      {/if}

      <span class="c-like" class:liked>
        {#if row.deezerId}
          <button
            class="icon-btn small like"
            class:on={liked}
            title={$t(liked ? 'menu.unlike' : 'menu.like')}
            aria-label={$t(liked ? 'menu.unlike' : 'menu.like')}
            aria-pressed={liked}
            on:click|stopPropagation={() => toggleLike(row.deezerId)}
          >
            {#if liked}<CircleCheck size={18} />{:else}<CirclePlus size={18} />{/if}
          </button>
        {/if}
      </span>

      <span class="c-dur num">{formatTime((row.durationMs || 0) / 1000)}</span>

      <span class="c-more">
        <button class="icon-btn small more" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: row.title })} on:click={(e) => menu(e, row)}>
          <Ellipsis size={18} />
        </button>
      </span>

      {#if reorderable}
        <span class="grip" aria-hidden="true"><GripVertical size={18} /></span>
      {/if}
    </div>
  {/each}
</div>

{#if editingTrack}
  {#await import('./EditDialog.svelte') then m}
    <svelte:component this={m.default} kind="track" deezerId={editingTrack} albumId={context?.kind === 'album' ? context.id : null} on:close={() => (editingTrack = null)} on:saved={() => ((editingTrack = null), dispatch('changed'))} />
  {/await}
{/if}

<style>
  .added-by {
    display: block;
    width: fit-content; /* ссылка на профиль — только по тексту, не по всей строке */
    max-width: 100%;
    font-size: 12px;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .added-by:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  /* # | название | (исполнители) | (альбом) | в любимые | длительность | … */
  .tracklist {
    --cols: 40px minmax(0, 1fr) 32px 48px 32px;
  }
  .tracklist.with-album {
    --cols: 40px minmax(0, 4fr) minmax(0, 3fr) 32px 48px 32px;
  }
  .tracklist.compact {
    --cols: 40px minmax(0, 3fr) minmax(0, 2fr) 32px 48px 32px;
  }
  .tracklist.compact.with-album {
    --cols: 40px minmax(0, 3fr) minmax(0, 2fr) minmax(0, 2fr) 32px 48px 32px;
  }
  .compact .artists {
    display: none; /* в компактном виде исполнители — своей колонкой */
  }
  .head,
  .row {
    display: grid;
    grid-template-columns: var(--cols);
    align-items: center;
    gap: 12px;
    padding: 0 12px;
  }
  .head {
    height: 36px;
    margin-bottom: 8px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.1);
    color: var(--text-dim);
    font-size: 14px;
  }
  /* Подписи столбцов — одним размером и цветом: правила столбцов ниже
     (исполнитель, альбом) — для строк, а не для шапки. */
  .head > span {
    font-size: inherit;
    color: inherit;
  }
  .disc {
    padding: 16px 12px 8px;
    color: var(--text-dim);
    font-size: 14px;
    font-weight: 600;
  }
  .row {
    min-height: 56px;
    border-radius: 6px;
    cursor: default;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none; /* долгое нажатие — наше меню, не системное */
  }
  .compact .row {
    min-height: 32px;
  }
  .row:focus-visible {
    background: rgba(255, 255, 255, 0.08);
  }
  @media (hover: hover) {
    .row:hover {
      background: rgba(255, 255, 255, 0.08);
    }
  }
  .row.selected {
    background: rgba(255, 255, 255, 0.16);
  }
  /* Перетаскивание: поднятая строка бледнеет, место вставки — линия. */
  .dragging,
  .dragging .row {
    cursor: grabbing;
  }
  .row.lifted {
    opacity: 0.4;
  }
  .row.drop-before {
    box-shadow: inset 0 2px 0 var(--accent);
  }
  .row.drop-after {
    box-shadow: inset 0 -2px 0 var(--accent);
  }
  /* Ручка — только на сенсорных экранах (мышью тянут за строку). */
  .grip {
    display: none;
  }
  .num-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    color: inherit;
  }
  .c-num {
    display: flex;
    justify-content: center;
    color: var(--text-dim);
  }
  .c-dur {
    text-align: right;
    justify-self: end;
    display: flex;
  }
  .hover-play {
    display: none;
    color: var(--text);
  }
  /* Номер → ▶ при наведении — только с мышью. Safari на iPhone, если
     «наведение» что-то прячет или показывает, первый тап отдаёт ему, и
     трек включался только со второго. */
  @media (hover: hover) {
    .row:hover .n {
      display: none;
    }
    .row:hover .hover-play {
      display: block;
    }
  }
  .c-title {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .cover {
    width: 40px;
    height: 40px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
    background: var(--bg-press);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .title-line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .title,
  .artists,
  .c-artist,
  .c-album {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .title {
    font-size: 15px;
    min-width: 0;
  }
  .row.playing .title {
    color: var(--page-accent, var(--accent));
  }
  .artists,
  .c-artist,
  .c-album {
    font-size: 13px;
    color: var(--text-dim);
  }
  .compact .c-artist {
    font-size: 14px;
  }
  @media (hover: hover) {
    .c-album a:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .hidden-mark {
    flex-shrink: 0;
    display: inline-flex;
    color: var(--text-dim);
  }
  .row.unavailable {
    opacity: 0.4;
    cursor: default;
  }
  @media (hover: hover) {
    .row.unavailable:hover {
      background: none;
    }
    .row.unavailable:hover .hover-play {
      display: none;
    }
    .row.unavailable:hover .n {
      display: inline;
    }
  }
  .explicit {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 2px;
    background: rgba(255, 255, 255, 0.6);
    color: #121212;
    font-size: 10px;
    font-weight: 700;
  }
  .c-like,
  .c-more {
    display: flex;
    justify-content: center;
    color: var(--text-dim);
  }
  .icon-btn.small {
    width: 28px;
    height: 28px;
  }
  /* «В любимые» и «…» — при наведении; галочка лайка видна всегда. */
  .like,
  .more {
    opacity: 0;
  }
  .row:focus-within .like,
  .row:focus-within .more,
  .like.on {
    opacity: 1;
  }
  @media (hover: hover) {
    .row:hover .like,
    .row:hover .more {
      opacity: 1;
    }
  }
  .like.on {
    color: var(--ok);
  }

  .eq {
    display: inline-flex;
    align-items: flex-end;
    gap: 2px;
    height: 14px;
  }
  .eq i {
    width: 3px;
    height: 100%;
    background: var(--page-accent, var(--accent));
    animation: eq 0.9s ease-in-out infinite;
    transform-origin: bottom;
  }
  .eq i:nth-child(2) {
    animation-delay: -0.3s;
  }
  .eq i:nth-child(3) {
    animation-delay: -0.6s;
  }
  @keyframes eq {
    0%,
    100% {
      transform: scaleY(0.3);
    }
    50% {
      transform: scaleY(1);
    }
  }

  @media (max-width: 768px) {
    /* Название забирает всю ширину: «в любимые» — только у лайкнутых
       (у остальных он всё равно не виден), длительности нет, как в Apple
       Music; не влезло — многоточие, а не вторая строка (строки скакали бы
       по высоте). Номер — только в треклисте альбома (там нет обложек). */
    .tracklist,
    .tracklist.with-album,
    .tracklist.compact,
    .tracklist.compact.with-album {
      --cols: minmax(0, 1fr) auto 32px;
    }
    .tracklist.album-numbers {
      --cols: 22px minmax(0, 1fr) auto 32px;
    }
    .head {
      display: none;
    }
    .tracklist:not(.album-numbers) .c-num,
    .c-album,
    .c-artist,
    .c-dur {
      display: none;
    }
    .row {
      padding: 0 8px;
      gap: 8px;
    }
    .c-num {
      justify-content: flex-start;
    }
    /* «В любимые» не у лайкнутых — нулевой ширины, но в сетке: убранный
       совсем, он отдал бы свою колонку «…», а справа осталась бы пустая. */
    .c-like:not(.liked) {
      width: 0;
      overflow: hidden;
    }
    .num-btn {
      width: 22px;
      justify-content: flex-start;
    }

    /* на телефоне исполнители всегда под названием, даже в компактном виде */
    .compact .artists {
      display: block;
    }
    .compact .row {
      min-height: 48px;
    }
  }
  /* На телефоне исполнитель под названием — не ссылка: палец, метивший в
     строку, попадал в маленькое имя и уводил на страницу артиста вместо
     того, чтобы включить трек. К артисту — через «…». */
  @media (max-width: 768px), (hover: none) {
    .artists :global(.artist-link),
    .c-artist :global(.artist-link) {
      pointer-events: none;
    }
  }
  /* На сенсорных экранах наведения нет: «…» видно всегда, плюс — только у лайкнутых. */
  @media (hover: none) {
    .more {
      opacity: 1;
    }
    .reorderable .head {
      grid-template-columns: var(--cols) 32px;
    }
    .reorderable .row {
      grid-template-columns: var(--cols) 32px;
    }
    .grip {
      display: flex;
      align-items: center;
      justify-content: center;
      height: 40px;
      color: var(--text-dim);
      touch-action: none; /* тянуть, а не прокручивать страницу */
    }
  }
</style>
