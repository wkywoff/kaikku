<script>
  // Шапка страницы альбома/артиста: большая картинка, тип («Альбом»,
  // «Исполнитель»), огромное название и строка метаданных (слот meta).
  // Фон — динамический цвет страницы (App.svelte), здесь только вёрстка.
  import { sized } from '../imageproxy.js';
  import { createEventDispatcher } from 'svelte';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import UserRound from '@lucide/svelte/icons/user-round';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import { t } from '../i18n.js';

  export let image = null;
  export let round = false;
  export let kind = '';
  export let title = '';
  export let zoomLabel = ''; // если задан — картинка кликабельна (событие zoom)
  export let hidden = false; // релиз скрыт админом — значок справа от названия

  const dispatch = createEventDispatcher();

  // Длинные названия — мельче, чтобы не занимали пол-экрана (Unbounded
  // широкий: пороги и размеры меньше, чем были для Onest).
  $: size = title.length > 32 ? 'm' : title.length > 14 ? 'l' : 'xl';
</script>

<header class="hero">
  <div class="art" class:round>
    {#if $$slots.art}
      <slot name="art" />
    {:else if image && zoomLabel}
      <button class="zoom" on:click={() => dispatch('zoom')} aria-label={zoomLabel} title={zoomLabel}>
        <img src={sized(image, round ? 320 : 300)} alt="" />
      </button>
    {:else if image}
      <img src={sized(image, round ? 320 : 300)} alt="" />
    {:else}
      <div class="placeholder">
        {#if round}<UserRound size={72} />{:else}<Disc3 size={72} />{/if}
      </div>
    {/if}
  </div>
  <div class="info">
    {#if kind}<span class="kind">{kind}</span>{/if}
    <h1 class={size}>{title}{#if hidden}<span class="hidden-mark" title={$t('admin.hiddenRelease')} aria-label={$t('admin.hiddenRelease')}><EyeOff /></span>{/if}</h1>
    <div class="meta"><slot name="meta" /></div>
  </div>
</header>

<style>
  .hero {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    padding: 24px 0 24px;
    min-height: 280px;
  }
  .art {
    width: 232px;
    height: 232px;
    flex-shrink: 0;
    border-radius: 6px;
    /* Короткая лёгкая тень, как у Apple Music: широкая тёмная (40 px) ложилась
       ореолом вокруг обложки, и на её спаде были видны ступеньки тонов. */
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.28);
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
  .zoom {
    display: block;
    width: 100%;
    height: 100%;
    border-radius: inherit;
    cursor: zoom-in;
    transition: transform 0.15s;
  }
  @media (hover: hover) {
    .zoom:hover {
      transform: scale(1.015);
    }
  }
  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .info {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .kind {
    font-size: 14px;
    font-weight: 500;
  }
  h1 {
    font-weight: 800;
    line-height: 1.05;
    letter-spacing: -0.03em;
    margin: 4px 0 10px;
    overflow-wrap: anywhere;
  }
  /* Значок скрытого релиза — по высоте строки названия. */
  .hidden-mark {
    display: inline-flex;
    vertical-align: middle;
    margin-left: 0.3em;
    color: rgba(255, 255, 255, 0.6);
  }
  .hidden-mark :global(svg) {
    width: 0.6em;
    height: 0.6em;
  }
  h1.xl {
    font-size: clamp(34px, 6vw, 80px);
  }
  h1.l {
    font-size: clamp(28px, 4.2vw, 54px);
  }
  h1.m {
    font-size: clamp(22px, 2.9vw, 38px);
  }
  .meta {
    font-size: 14px;
    color: rgba(255, 255, 255, 0.85);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }

  @media (max-width: 768px) {
    .hero {
      flex-direction: column;
      align-items: center;
      text-align: center;
      min-height: 0;
      gap: 16px;
      padding-top: 12px;
    }
    .art {
      width: min(62vw, 240px);
      height: min(62vw, 240px);
    }
    .info {
      align-items: center;
    }
    .meta {
      justify-content: center;
    }
  }
</style>
