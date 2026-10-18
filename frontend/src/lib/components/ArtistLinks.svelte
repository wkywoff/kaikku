<script>
  // Исполнители через запятую, каждый — отдельная ссылка на свою страницу.
  // С menu — у имени своё меню (правый клик, долгое нажатие): перейти к
  // исполнителю и скопировать ссылку.
  import { get } from 'svelte/store';
  import { link } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { longpress } from '../longpress.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { linkItems } from '../share.js';
  import UserRound from '@lucide/svelte/icons/user-round';

  export let artists = []; // [{ name, href? }]
  export let onNavigate = null; // например, закрыть полноэкранный плеер
  export let menu = false;

  function artistMenu(e, a) {
    openMenu(e, [{ label: get(t)('menu.goToArtist'), icon: UserRound, href: a.href }, ...linkItems(a.href, a.name)]);
  }
  // Долгое нажатие на имени — меню исполнителя, а не всего трека вокруг.
  const own = (e) => menu && e.pointerType !== 'mouse' && e.stopPropagation();
  const noop = () => {};
</script>

{#each artists as a, i (i)}{#if i > 0}{', '}{/if}{#if a.href}<a
      class="artist-link"
      href={a.href}
      use:link
      use:longpress={menu ? (e) => artistMenu(e, a) : noop}
      on:pointerdown={own}
      on:contextmenu={(e) => menu && artistMenu(e, a)}
      on:click={() => onNavigate?.()}>{a.name}</a
    >{:else}<span>{a.name}</span>{/if}{/each}

<style>
  @media (hover: hover) {
    .artist-link:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
</style>
