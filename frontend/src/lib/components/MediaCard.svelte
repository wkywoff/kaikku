<script>
  // Карточка альбома/артиста для сеток: обложка, название, подпись. Если
  // передан onPlay — при наведении появляется кнопка Play (как у Spotify).
  // menu — пункты её меню (lib/cardMenu.js): правый клик, долгое нажатие
  // пальцем, а с мышью ещё и «…» при наведении.
  import { sized, srcsetOf, CARD_SIZES } from '../imageproxy.js';
  import { link, fromHistory } from '../stores/nav.js';
  import PlayButton from './PlayButton.svelte';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import UserRound from '@lucide/svelte/icons/user-round';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import { t } from '../i18n.js';
  import { canEdit } from '../stores/me.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { longpress } from '../longpress.js';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';

  export let href;
  export let image = null;
  export let title;
  export let subtitle = '';
  export let round = false;
  export let onPlay = null;
  export let playing = false;
  export let hidden = false; // релиз скрыт админом: приглушён, со значком
  export let eager = false; // виден сразу (первый ряд главной): грузить не лениво
  export let first = false; // главная картинка экрана (LCP): раньше остальных
  export let menu = null; // () => пункты меню

  const showMenu = (e) => menu && openMenu(e, menu());

  // Страницу открыли «назад»/«вперёд»: картинки уже в кеше — сразу, без
  // отложенной загрузки (Safari рисовал их сначала пустыми).
  const restored = fromHistory();
</script>

<a class="card" class:hidden {href} use:link on:contextmenu={showMenu} use:longpress={showMenu}>
  <div class="art" class:round>
    {#if $$slots.art}
      <slot name="art" />
    {:else if image}
      <!-- loading, sizes и srcset — раньше src: иначе браузер может начать
           грузить src сразу, мимо отложенной загрузки и выбора размера. -->
      <img
        loading={eager || restored ? 'eager' : 'lazy'}
        fetchpriority={first ? 'high' : 'auto'}
        sizes={CARD_SIZES}
        srcset={srcsetOf(image, round ? [200, 320] : [200, 300])}
        src={sized(image, round ? 320 : 300)}
        alt=""
      />
    {:else}
      <div class="placeholder">
        {#if round}<UserRound size={40} />{:else}<Disc3 size={40} />{/if}
      </div>
    {/if}
    {#if hidden}<span class="hidden-badge" title={$t('admin.hiddenRelease')}><EyeOff size={16} /></span>{/if}
    {#if menu}
      <button class="more" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title })} on:click|preventDefault|stopPropagation={showMenu}><Ellipsis size={18} /></button>
    {/if}
    {#if onPlay && (!hidden || $canEdit)}
      <div class="play" class:visible={playing}>
        <PlayButton size={46} {playing} on:click={(e) => (e.preventDefault(), e.stopPropagation(), onPlay())} />
      </div>
    {/if}
  </div>
  <div class="title" title={title}>{title}</div>
  {#if subtitle}<div class="subtitle">{subtitle}</div>{/if}
</a>

<style>
  .card.hidden .art > :global(img),
  .card.hidden .title,
  .card.hidden .subtitle {
    opacity: 0.45;
  }
  .hidden-badge {
    position: absolute;
    top: 8px;
    right: 8px;
    display: flex;
    padding: 6px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.7);
    color: #fff;
  }
  .card {
    -webkit-touch-callout: none; /* долгое нажатие — наше меню, не системное */
    display: block;
    padding: 12px;
    border-radius: var(--radius);
    transition: background-color 0.2s;
    min-width: 0;
  }
  @media (hover: hover) {
    .card:hover {
      background: var(--bg-card);
    }
  }
  .art {
    position: relative;
    aspect-ratio: 1;
    margin-bottom: 10px;
    border-radius: 6px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  }
  .art.round {
    border-radius: 50%;
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
  .play {
    position: absolute;
    right: 8px;
    bottom: 8px;
    opacity: 0;
    transform: translateY(8px);
    transition:
      opacity 0.2s,
      transform 0.2s;
  }
  .play.visible {
    opacity: 1;
    transform: none;
  }
  @media (hover: hover) {
    .card:hover .play {
      opacity: 1;
      transform: none;
    }
  }
  /* «…» — только с мышью и при наведении; на телефоне меню — долгим нажатием. */
  .more {
    position: absolute;
    top: 8px;
    right: 8px;
    display: none;
    padding: 6px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.6);
    color: #fff;
  }
  @media (hover: hover) {
    .card:hover .more {
      display: flex;
    }
    .more:hover {
      background: rgba(0, 0, 0, 0.8);
    }
  }
  .title {
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .subtitle {
    margin-top: 2px;
    font-size: 13px;
    color: var(--text-dim);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  @media (hover: none) {
    .play {
      opacity: 1;
      transform: none;
    }
    /* Название микса на полосе обложки (Artwork) — не под кнопкой Play. */
    .art:has(.play) :global(.mix .band) {
      padding-right: 58px;
    }
  }
  .art:has(.play.visible) :global(.mix .band) {
    padding-right: 58px;
  }
</style>
