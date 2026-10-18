<script>
  // Контекстное меню (правый клик по треку или кнопка «…»), как в Spotify.
  // Одно на всё приложение — App.svelte; что показать, задаёт
  // stores/contextMenu.js → openMenu. Пункт с submenu раскрывает вложенное
  // меню сбоку (наведением или кликом — на телефоне наведения нет). Меню не
  // вылезает за края экрана; закрывается по Esc, клику мимо и смене окна.
  import { tick } from 'svelte';
  import { contextMenu, closeMenu } from '../stores/contextMenu.js';
  import { navigateAfterPaint } from '../stores/nav.js';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Check from '@lucide/svelte/icons/check';

  let menuEl;
  let subEl;
  let pos = null; // { x, y }
  let openSub = -1;
  let subPos = null;
  let anchor = null; // элемент пункта, у которого открыто подменю

  $: menu = $contextMenu;
  $: place(menu);

  async function place(m) {
    openSub = -1;
    pos = null;
    if (!m) return;
    await tick();
    if (!menuEl) return;
    const r = menuEl.getBoundingClientRect();
    const x = Math.max(8, Math.min(m.x, innerWidth - r.width - 8));
    const y = m.y + r.height > innerHeight - 8 ? Math.max(8, m.y - r.height) : m.y;
    pos = { x, y };
  }

  async function showSub(i, el) {
    if (openSub === i) return;
    openSub = i;
    anchor = el;
    subPos = null;
    await tick();
    if (!subEl || !anchor) return;
    const a = anchor.getBoundingClientRect();
    const r = subEl.getBoundingClientRect();
    const x = a.right + r.width + 4 <= innerWidth - 8 ? a.right + 4 : Math.max(8, a.left - r.width - 4);
    const y = Math.max(8, Math.min(a.top - 4, innerHeight - r.height - 8));
    subPos = { x, y };
  }

  function run(item) {
    closeMenu();
    if (item.href) navigateAfterPaint(item.href);
    else item.onClick?.();
  }

  function onKey(e) {
    if (e.key === 'Escape' && menu) closeMenu();
  }

  // Закрываем, только если окно поменяло ширину: на телефоне высота
  // меняется сама, когда браузер прячет адресную строку при прокрутке.
  let lastWidth = typeof window !== 'undefined' ? window.innerWidth : 0;
  function onResize() {
    if (window.innerWidth !== lastWidth) closeMenu();
    lastWidth = window.innerWidth;
  }
</script>

<svelte:window on:keydown={onKey} on:resize={onResize} on:blur={closeMenu} on:popstate={closeMenu} />

{#if menu}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="backdrop" on:pointerdown={closeMenu} on:contextmenu|preventDefault={closeMenu}></div>
  <div class="menu" role="menu" bind:this={menuEl} style="left: {pos?.x ?? menu.x}px; top: {pos?.y ?? menu.y}px; visibility: {pos ? 'visible' : 'hidden'}">
    {#each menu.items as item, i}
      {#if item.separator}
        <div class="sep"></div>
      {:else if item.heading}
        <div class="heading">{item.heading}</div>
      {:else}
        <button
          class="item"
          class:danger={item.danger}
          class:checked={item.checked}
          class:open={openSub === i}
          role="menuitem"
          aria-haspopup={item.submenu ? 'menu' : undefined}
          aria-expanded={item.submenu ? openSub === i : undefined}
          on:mouseenter={(e) => (item.submenu ? showSub(i, e.currentTarget) : (openSub = -1))}
          on:click={(e) => (item.submenu ? showSub(i, e.currentTarget) : run(item))}
        >
          {#if item.icon}<svelte:component this={item.icon} size={16} />{:else if item.checked === undefined}<span class="no-icon"></span>{/if}
          <span class="label">{item.label}</span>
          {#if item.submenu}<ChevronRight size={16} />{/if}
          {#if item.checked}<Check size={18} />{/if}
        </button>
      {/if}
    {/each}
  </div>

  {#if openSub >= 0 && menu.items[openSub]?.submenu}
    <div class="menu sub" role="menu" bind:this={subEl} style="left: {subPos?.x ?? 0}px; top: {subPos?.y ?? 0}px; visibility: {subPos ? 'visible' : 'hidden'}">
      {#each menu.items[openSub].submenu as item}
        {#if item.separator}
          <div class="sep"></div>
        {:else}
          <button class="item" class:checked={item.checked} role="menuitem" on:click={() => run(item)}>
            {#if item.icon}<svelte:component this={item.icon} size={16} />{/if}
            <span class="label">{item.label}</span>
            {#if item.checked}<Check size={18} />{/if}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 90;
  }
  .menu {
    position: fixed;
    z-index: 91;
    min-width: 220px;
    max-width: min(340px, calc(100vw - 16px));
    max-height: calc(100vh - 16px);
    overflow-y: auto;
    padding: 4px;
    border-radius: 6px;
    background: #282828;
    box-shadow: 0 16px 24px rgba(0, 0, 0, 0.3), 0 6px 8px rgba(0, 0, 0, 0.2);
  }
  .item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 10px 10px 12px;
    border-radius: 3px;
    font-size: 14px;
    color: rgba(255, 255, 255, 0.9);
    text-align: left;
  }
  .item.open,
  .item:focus-visible {
    background: rgba(255, 255, 255, 0.1);
    color: #fff;
    outline: none;
  }
  @media (hover: hover) {
    .item:hover {
      background: rgba(255, 255, 255, 0.1);
      color: #fff;
      outline: none;
    }
  }
  .item.danger {
    color: var(--danger);
  }
  .item.checked {
    color: var(--ok);
  }
  .heading {
    padding: 10px 12px 6px;
    font-size: 12px;
    font-weight: 700;
    color: var(--text-dim);
  }
  .label {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .no-icon {
    width: 16px;
    flex-shrink: 0;
  }
  .sep {
    height: 1px;
    margin: 4px 0;
    background: rgba(255, 255, 255, 0.1);
  }
</style>
