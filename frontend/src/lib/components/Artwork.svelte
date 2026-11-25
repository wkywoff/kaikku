<script>
  import { sized, srcsetOf, CARD_SIZES } from '../imageproxy.js';
  import { fromHistory } from '../stores/nav.js';
  import ListMusic from '@lucide/svelte/icons/list-music';

  export let kind = 'playlist';
  export let label = '';
  export let hue = 0;
  export let images = [];
  export let iconSize = 64;
  export let sizes = CARD_SIZES;
  export let tileSizes = '(max-width: 768px) calc(25vw - 16px), 90px';
  export let eager = false;
  const restored = fromHistory();

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
