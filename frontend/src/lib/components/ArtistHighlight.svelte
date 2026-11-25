<script>
  import { sized } from '../imageproxy.js';
  import { link } from '../stores/nav.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { longpress } from '../longpress.js';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import Heart from '@lucide/svelte/icons/heart';
  import UserRound from '@lucide/svelte/icons/user-round';

  export let href;
  export let label;
  export let title;
  export let subtitle = '';
  export let image = null;
  export let liked = false;
  export let menu = null;

  const showMenu = (e) => menu && openMenu(e, menu(), { title, subtitle, image });
</script>

<a class="highlight" class:liked {href} use:link on:contextmenu={showMenu} use:longpress={showMenu}>
  <h2>{label}</h2>
  <span class="art">
    {#if image}
      <img src={sized(image, 200)} alt="" />
    {:else}
      <span class="placeholder">{#if liked}<UserRound size={32} />{:else}<Disc3 size={32} />{/if}</span>
    {/if}
    {#if liked}<span class="badge"><Heart size={18} fill="currentColor" /></span>{/if}
  </span>
  <span class="text">
    <span class="title">{title}</span>
    {#if subtitle}<span class="subtitle">{subtitle}</span>{/if}
  </span>
</a>

<style>
  .highlight {
    display: grid;
    grid-template-columns: 88px minmax(0, 1fr);
    column-gap: 16px;
    align-items: center;
    -webkit-touch-callout: none;
  }
  h2 {
    grid-column: 1 / -1;
    margin-bottom: 16px;
    font-size: 24px;
  }
  .art {
    position: relative;
    display: block;
    width: 100%;
    aspect-ratio: 1;
    border-radius: 6px;
  }
  .liked .art {
    border-radius: 50%;
  }
  .art img,
  .placeholder {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
  }
  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .badge {
    position: absolute;
    right: -4px;
    bottom: -4px;
    display: flex;
    padding: 5px;
    border-radius: 50%;
    background: var(--bg);
    color: var(--ok);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .title,
  .subtitle {
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .title {
    font-size: 16px;
    font-weight: 700;
    line-height: 1.3;
  }
  .subtitle {
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .highlight:hover .title {
      text-decoration: underline;
    }
  }
  @media (max-width: 768px) {
    .highlight {
      grid-template:
        'art label' auto
        'art text' auto
        / 56px minmax(0, 1fr);
      column-gap: 12px;
    }
    h2 {
      grid-area: label;
      align-self: end;
      margin: 0;
      font-family: var(--font);
      font-size: 13px;
      font-weight: 400;
      letter-spacing: 0;
      color: var(--text-dim);
    }
    .art {
      grid-area: art;
    }
    .badge {
      padding: 4px;
    }
    .badge :global(svg) {
      width: 12px;
      height: 12px;
    }
    .text {
      grid-area: text;
      align-self: start;
      gap: 0;
    }
    .title {
      -webkit-line-clamp: 1;
      line-clamp: 1;
    }
    .subtitle {
      font-size: 13px;
    }
    .liked .subtitle {
      display: none;
    }
  }
</style>
