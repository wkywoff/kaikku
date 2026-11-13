<script>
  // Меню на телефоне — шторка снизу, как в Spotify: сверху — о чём меню
  // (обложка, название), ниже крупные строки под палец. Подменю открывается
  // на месте списка, со строкой «назад». Закрывается нажатием мимо, свайпом
  // вниз, жестом «назад» (stores/contextMenu.js) и Esc (ContextMenu).
  import { onDestroy } from 'svelte';
  import { fade } from 'svelte/transition';
  import { cubicOut, quadIn } from 'svelte/easing';
  import { closeMenu, closedByBack } from '../stores/contextMenu.js';
  import { navigateAfterPaint } from '../stores/nav.js';
  import { focusTrap } from '../focusTrap.js';
  import { appleTouch } from '../platform.js';
  import { dragDismiss } from '../sheetDrag.js';
  import { sized } from '../imageproxy.js';
  import { t } from '../i18n.js';
  import ChevronLeft from '@lucide/svelte/icons/chevron-left';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Check from '@lucide/svelte/icons/check';

  export let menu; // { items, header } — см. stores/contextMenu.js

  // Пока шторка уезжает, меню в хранилище уже нет — рисуем последнее.
  let shown = menu;
  let sub = null; // пункт, чьё подменю открыто
  $: if (menu && menu !== shown) {
    shown = menu;
    sub = null;
  }
  $: header = shown.header;
  $: list = sub ? sub.submenu : shown.items;
  // Значки — одной колонкой: строкам без значка оставляем под него место,
  // если в этом списке значки вообще есть.
  $: hasIcons = list.some((item) => item.icon);

  let root;
  let backdrop;
  let listEl;
  let dragY = 0; // на сколько шторку стянули вниз пальцем

  // Переход — когда шторка уехала: айфон снимает страницу для свайпа
  // «назад» в момент перехода, и шторка осталась бы на снимке.
  let href = null;
  function leave() {
    if (!href) return;
    const to = href;
    href = null;
    navigateAfterPaint(to);
  }
  onDestroy(leave);

  function run(item) {
    if (item.submenu) {
      sub = item;
      if (listEl) listEl.scrollTop = 0;
      return;
    }
    closeMenu();
    if (item.href) href = item.href;
    else item.onClick?.();
  }

  // Меню открыли долгим нажатием — палец ещё на экране, а шторка уже под
  // ним. Его отпускание — не нажатие: ни пункт под пальцем, ни закрытие.
  // Считаются только нажатия, начатые на самой шторке (и Enter с клавиатуры:
  // у такого click detail = 0).
  let pressed = false;
  function onClickCapture(e) {
    if (pressed || e.detail === 0) return;
    e.preventDefault();
    e.stopPropagation();
  }

  function onDrag(dy, height) {
    dragY = dy;
    if (backdrop) backdrop.style.opacity = dy ? String(1 - 0.6 * Math.min(1, dy / (height || 1))) : '';
  }

  const rise = (node) => {
    const h = node.offsetHeight;
    return { duration: 280, easing: cubicOut, css: (_t, u) => `transform: translate3d(0, ${u * h}px, 0)` };
  };
  // Свайп от края экрана на iPhone — системный «назад»: он сам уводит
  // страницу со шторкой вбок за пальцем. Анимация закрытия после него
  // показала бы шторку ещё раз — убираем сразу (как полноэкранный плеер).
  const gone = () => closedByBack() && appleTouch;
  // Уезжает с того места, где её отпустили (dragY).
  const drop = (node) => {
    if (gone()) return { duration: 0 };
    const h = node.offsetHeight;
    const from = dragY;
    return { duration: 200, easing: quadIn, css: (_t, u) => `transform: translate3d(0, ${from + u * (h - from)}px, 0)` };
  };
  const dim = (node) => (gone() ? { duration: 0 } : fade(node, { duration: 200 }));
</script>

<!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
<div class="layer" bind:this={root} on:pointerdown|capture={() => (pressed = true)} on:click|capture={onClickCapture}>
  <div class="backdrop" bind:this={backdrop} in:fade={{ duration: 200 }} out:dim on:click={closeMenu} on:contextmenu|preventDefault={closeMenu}></div>
  <div
    class="sheet"
    role="dialog"
    aria-modal="true"
    aria-label={sub?.label || header?.title || $t('menu.more')}
    use:focusTrap
    use:dragDismiss={{ scroller: () => listEl, onDrag, onDismiss: closeMenu }}
    in:rise
    out:drop
    on:outrostart={() => (root.inert = true)}
    on:outroend={leave}
  >
    <div class="grip"></div>
    {#if sub}
      <button class="back" on:click={() => (sub = null)} aria-label={`${$t('menu.back')}: ${sub.label}`}>
        <ChevronLeft size={24} />
        <span class="label">{sub.label}</span>
      </button>
    {:else if header}
      <div class="head">
        {#if header.image}<img class:round={header.round} src={sized(header.image, 200)} alt="" />{/if}
        <div class="about">
          <div class="title">{header.title}</div>
          {#if header.subtitle}<div class="subtitle">{header.subtitle}</div>{/if}
        </div>
      </div>
    {/if}
    <div class="list" role="menu" bind:this={listEl}>
      {#each list as item}
        {#if item.separator}
          <div class="sep"></div>
        {:else if item.heading}
          <div class="heading">{item.heading}</div>
        {:else}
          <button class="item" class:danger={item.danger} class:checked={item.checked} role="menuitem" aria-haspopup={item.submenu ? 'menu' : undefined} on:click={() => run(item)}>
            {#if item.icon}<svelte:component this={item.icon} size={22} />{:else if hasIcons}<span class="no-icon"></span>{/if}
            <span class="label">{item.label}</span>
            {#if item.submenu}<span class="hint"><ChevronRight size={20} /></span>{/if}
            {#if item.checked}<Check size={22} />{/if}
          </button>
        {/if}
      {/each}
    </div>
  </div>
</div>

<style>
  .layer {
    position: fixed;
    inset: 0;
    z-index: 90;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    align-items: center;
  }
  .backdrop {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    touch-action: none; /* страница под шторкой не листается */
  }
  .sheet {
    position: relative;
    width: 100%;
    max-width: 560px;
    max-height: 85vh;
    max-height: 85dvh; /* видимая высота: без адресной строки браузера */
    display: flex;
    flex-direction: column;
    padding-bottom: max(8px, env(safe-area-inset-bottom));
    border-radius: 16px 16px 0 0;
    background: #282828;
    box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.35);
    outline: none;
    will-change: transform;
    -webkit-user-select: none;
    user-select: none;
    -webkit-touch-callout: none;
  }
  .grip {
    flex-shrink: 0;
    width: 36px;
    height: 4px;
    margin: 8px auto 4px;
    border-radius: 2px;
    background: rgba(255, 255, 255, 0.3);
  }
  .head {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 10px 20px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.1);
  }
  .head img {
    width: 48px;
    height: 48px;
    flex-shrink: 0;
    border-radius: 4px;
    object-fit: cover;
    background: var(--bg-press);
  }
  .head img.round {
    border-radius: 50%;
  }
  .about {
    min-width: 0;
  }
  .title,
  .subtitle {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .title {
    font-size: 16px;
    font-weight: 700;
  }
  .subtitle {
    margin-top: 2px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .back {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 56px;
    padding: 4px 20px 8px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.1);
    font-size: 16px;
    font-weight: 700;
    text-align: left;
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 6px 0;
  }
  .item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 16px;
    min-height: 52px;
    padding: 8px 20px;
    font-size: 16px;
    color: rgba(255, 255, 255, 0.92);
    text-align: left;
  }
  .item:active,
  .back:active,
  .item:focus-visible,
  .back:focus-visible {
    background: rgba(255, 255, 255, 0.08);
    outline: none;
  }
  .item.danger {
    color: var(--danger);
  }
  .item.checked {
    color: var(--ok);
  }
  .label {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .no-icon {
    width: 22px;
    flex-shrink: 0;
  }
  .hint {
    display: flex;
    color: var(--text-faint);
  }
  .heading {
    padding: 12px 20px 6px;
    font-size: 13px;
    font-weight: 700;
    color: var(--text-dim);
  }
  .sep {
    height: 1px;
    margin: 6px 20px;
    background: rgba(255, 255, 255, 0.1);
  }
</style>
