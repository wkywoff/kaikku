<script>
  // Обложка для того, у чего нет своей картинки:
  //   liked    — «Любимые треки»: сердце из столбиков эквалайзера в цветах
  //              значка приложения;
  //   playlist — мозаика 2×2 из обложек треков (если их 4+), иначе первая
  //              обложка, а у пустого — значок;
  //   mix      — микс (lib/mixes.js): первая обложка и цветная полоса
  //              с названием, как Daily Mix у Spotify.
  // Заполняет родителя (квадрат задаёт Hero/MediaCard).
  import { sized, srcsetOf, CARD_SIZES } from '../imageproxy.js';
  import { fromHistory } from '../stores/nav.js';
  import ListMusic from '@lucide/svelte/icons/list-music';

  export let kind = 'playlist'; // 'liked' | 'playlist' | 'mix'
  export let label = ''; // mix: название на полосе
  export let hue = 0; // mix: оттенок полосы
  export let images = [];
  export let iconSize = 64;
  // Ширина всей картинки и клетки мозаики (атрибут sizes): по умолчанию —
  // карточка; шапка плейлиста передаёт свои.
  export let sizes = CARD_SIZES;
  export let tileSizes = '(max-width: 768px) calc(25vw - 16px), 90px';
  export let eager = false; // видна сразу (первый ряд главной): грузить не лениво
  const restored = fromHistory(); // открыли «назад»/«вперёд»: картинки уже в кеше (MediaCard)

  // Столбики сердца в квадрате 100×100: x и y левого верхнего угла, высота.
  const BARS = [
    [15, 32, 16],
    [23, 24, 34],
    [31, 22, 44],
    [39, 26, 47],
    [47, 34, 45],
    [55, 26, 47],
    [63, 22, 44],
    [71, 24, 34],
    [79, 32, 16],
  ];
</script>

{#if kind === 'liked'}
  <svg class="art liked" viewBox="0 0 100 100" aria-hidden="true">
    {#each BARS as [x, y, h]}<rect {x} {y} width="6" height={h} rx="3" />{/each}
  </svg>
{:else if kind === 'mix'}
  <div class="art mix" style:--mix-hue={hue}>
    {#if images.length}<img loading={eager || restored ? 'eager' : 'lazy'} {sizes} srcset={srcsetOf(images[0], [200, 300])} src={sized(images[0], 300)} alt="" />{/if}
    <span class="band"><span class="label">{label}</span></span>
  </div>
{:else if images.length >= 4}
  <div class="art mosaic">
    {#each images.slice(0, 4) as src}<img loading={eager || restored ? 'eager' : 'lazy'} sizes={tileSizes} srcset={srcsetOf(src, [200, 300])} src={sized(src, 300)} alt="" />{/each}
  </div>
{:else if images.length}
  <img class="art" loading={eager || restored ? 'eager' : 'lazy'} {sizes} srcset={srcsetOf(images[0], [200, 300])} src={sized(images[0], 300)} alt="" />
{:else}
  <div class="art empty"><ListMusic size={iconSize} /></div>
{/if}

<style>
  .art {
    width: 100%;
    height: 100%;
    display: block;
    border-radius: inherit;
    object-fit: cover;
  }
  .liked {
    background: #ff4d2e;
    fill: #141414;
  }
  .mix {
    position: relative;
    overflow: hidden;
    container-type: inline-size;
    background: hsl(var(--mix-hue) 45% 30%);
  }
  .mix img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  /* Полоса снизу в цвет микса, на ней название — тёмным, как у Spotify. */
  .mix .band {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    min-height: 30%;
    display: flex;
    align-items: center;
    padding: 4cqi 7cqi;
    background: hsl(var(--mix-hue) 70% 62%);
    box-shadow: 0 -1px 0 hsl(var(--mix-hue) 70% 62%);
  }
  .mix .label {
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 9cqi;
    line-height: 1.15;
    letter-spacing: -0.02em;
    color: #121212;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .mosaic {
    display: grid;
    grid-template-columns: 1fr 1fr;
    overflow: hidden;
  }
  .mosaic img {
    width: 100%;
    height: 100%;
    aspect-ratio: 1;
    object-fit: cover;
    display: block;
  }
  .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
</style>
