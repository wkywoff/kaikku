<script>
  import { sized } from '../imageproxy.js';
  import { player, queueState } from '../stores/player.js';
  import { contextTitle } from '../stores/sidePlayer.js';
  import { settings } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import Repeat from '@lucide/svelte/icons/repeat';
  import Repeat1 from '@lucide/svelte/icons/repeat-1';
  import InfinityIcon from '@lucide/svelte/icons/infinity';
  import AlignJustify from '@lucide/svelte/icons/align-justify';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import X from '@lucide/svelte/icons/x';

  export let compact = false;

  $: s = $queueState;
  $: current = s.queue[s.index];
  $: after = s.queue.map((item, i) => ({ item, i })).filter(({ i }) => i > s.index);
  $: sections = [
    { key: 'next', title: $t('queue.continue'), sub: fromLabel(current, $t), list: after.filter(({ item }) => !item.autoplay) },
    { key: 'auto', title: $t('queue.autoplay'), sub: $t('queue.autoplayHint'), list: after.filter(({ item }) => item.autoplay) },
  ].filter((sec) => sec.list.length);

  function fromLabel(cur, tr) {
    const name = contextTitle(cur, tr);
    return name ? tr('queue.from', { name }) : '';
  }

  let drag = null;
  function startDrag(e, i, list) {
    if (e.button !== 0) return;
    e.preventDefault();
    const row = e.currentTarget.closest('.slot');
    drag = { i, lo: list[0].i, hi: list[list.length - 1].i, y0: e.clientY, dy: 0, h: row.offsetHeight, id: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function moveDrag(e) {
    if (drag && e.pointerId === drag.id) drag = { ...drag, dy: e.clientY - drag.y0 };
  }
  function endDrag(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const { i } = drag;
    const to = targetOf({ ...drag, dy: e.clientY - drag.y0 });
    drag = null;
    if (to !== i) player.moveInQueueTo(i, to);
  }
  const targetOf = (d) => (d ? Math.max(d.lo, Math.min(d.hi, d.i + Math.round(d.dy / d.h))) : -1);
  $: target = targetOf(drag);
  function shift(i, d, to) {
    if (!d) return 0;
    if (i === d.i) return d.dy;
    if (d.i < to && i > d.i && i <= to) return -d.h;
    if (d.i > to && i < d.i && i >= to) return d.h;
    return 0;
  }
  function dragKey(e, i, list) {
    const dir = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    const to = i + dir;
    if (to >= list[0].i && to <= list[list.length - 1].i) player.moveInQueueTo(i, to);
  }

  let swipe = null;
  let swiped = false;
  function rowDown(e, i) {
    if (e.button !== 0 || (e.target instanceof Element && e.target.closest('.handle, .remove'))) return;
    swipe = { i, x0: e.clientX, y0: e.clientY, dx: 0, w: e.currentTarget.offsetWidth, on: false, id: e.pointerId };
  }
  function rowMove(e) {
    if (!swipe || e.pointerId !== swipe.id) return;
    const dx = e.clientX - swipe.x0;
    const dy = e.clientY - swipe.y0;
    if (!swipe.on) {
      if (Math.abs(dy) > 10 && Math.abs(dy) > Math.abs(dx)) return void (swipe = null);
      if (dx > -10 || Math.abs(dx) < Math.abs(dy)) return;
      e.currentTarget.setPointerCapture(e.pointerId);
      swipe = { ...swipe, on: true };
    }
    swipe = { ...swipe, dx: Math.min(0, dx) };
  }
  function rowUp(e) {
    if (!swipe || e.pointerId !== swipe.id) return;
    const { i, dx, w, on } = swipe;
    swipe = null;
    if (!on) return;
    swiped = true;
    setTimeout(() => (swiped = false), 0);
    if (dx < -w * 0.35) player.removeFromQueue(i);
  }
  function pick(i) {
    if (!swiped) player.jumpTo(i);
  }
</script>

<div class="queue-view" class:compact>
  <div class="pills">
    <button class="pill" class:on={s.shuffle} on:click={() => player.toggleShuffle()} aria-pressed={s.shuffle} title={$t('player.shuffle')} aria-label={$t('player.shuffle')}><Shuffle size={20} /></button>
    <button class="pill" class:on={s.repeat !== 'off'} on:click={() => player.cycleRepeat()} aria-pressed={s.repeat !== 'off'} title={$t(s.repeat === 'one' ? 'player.repeatOne' : s.repeat === 'all' ? 'player.repeatAll' : 'player.repeat')} aria-label={$t('player.repeat')}>
      {#if s.repeat === 'one'}<Repeat1 size={20} />{:else}<Repeat size={20} />{/if}
    </button>
    <button class="pill" class:on={$settings.autoplay} on:click={() => settings.set('autoplay', !$settings.autoplay)} aria-pressed={$settings.autoplay} title={$t('queue.autoplayHint')} aria-label={$t('queue.autoplay')}><InfinityIcon size={24} /></button>
  </div>

  <div class="list" class:dragging={!!drag}>
    {#each sections as sec (sec.key)}
      <div class="sec-head">
        <h3>{sec.title}</h3>
        {#if sec.sub}<p>{sec.sub}</p>{/if}
      </div>
      {#each sec.list as { item, i } (i + ':' + (item.id || item.deezerId))}
        <div class="slot" class:lifted={drag?.i === i} style="transform: translateY({shift(i, drag, target)}px)">
          <div class="swipe-bg" aria-hidden="true"><Trash2 size={20} /></div>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="row"
            style="transform: translateX({swipe?.i === i ? swipe.dx : 0}px)"
            class:swiping={swipe?.i === i && swipe.on}
            on:pointerdown={(e) => rowDown(e, i)}
            on:pointermove={rowMove}
            on:pointerup={rowUp}
            on:pointercancel={() => (swipe = null)}
          >
            <button class="pick" on:click={() => pick(i)} aria-label={`${$t('player.play')}: ${item.title}`}>
              {#if item.coverUrl}<img src={sized(item.coverUrl, 300)} alt="" loading="lazy" draggable="false" />{:else}<span class="ph"></span>{/if}
              <span class="meta"><span class="title">{item.title}</span><span class="artist">{item.artist}</span></span>
            </button>
            <button class="icon-btn remove" on:click={() => player.removeFromQueue(i)} aria-label={$t('queue.remove')} title={$t('queue.remove')}><X size={18} /></button>
            <span
              class="handle"
              role="button"
              tabindex="0"
              aria-label={$t('queue.drag')}
              title={$t('queue.drag')}
              on:pointerdown={(e) => startDrag(e, i, sec.list)}
              on:pointermove={moveDrag}
              on:pointerup={endDrag}
              on:pointercancel={endDrag}
              on:keydown={(e) => dragKey(e, i, sec.list)}><AlignJustify size={22} /></span
            >
          </div>
        </div>
      {/each}
    {:else}
      <p class="empty">{$t('queue.empty')}</p>
    {/each}
  </div>
</div>

<style>
  .queue-view {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 0;
  }
  .pills {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    flex-shrink: 0;
  }
  .pill {
    height: 44px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.14);
    color: var(--text);
    transition:
      background-color 0.15s,
      color 0.15s;
  }
  @media (hover: hover) {
    .pill:hover {
      background: rgba(255, 255, 255, 0.2);
    }
  }
  .pill.on {
    background: rgba(255, 255, 255, 0.85);
    color: var(--player-dark);
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
    -webkit-mask-image: linear-gradient(180deg, #000 calc(100% - 32px), transparent);
    mask-image: linear-gradient(180deg, #000 calc(100% - 32px), transparent);
    padding-bottom: 32px;
  }
  .list::-webkit-scrollbar {
    display: none;
  }
  .sec-head {
    margin: 4px 0 10px;
  }
  .sec-head:not(:first-child) {
    margin-top: 24px;
  }
  h3 {
    font-size: 20px;
    font-weight: 700;
    letter-spacing: -0.01em;
  }
  .sec-head p {
    margin-top: 2px;
    font-size: 14px;
    color: rgba(255, 255, 255, 0.55);
  }
  .slot {
    position: relative;
    border-radius: 6px;
    transition: transform 0.15s ease;
  }
  .list.dragging .slot.lifted {
    z-index: 1;
    transition: none;
  }
  .lifted .row {
    background: rgba(255, 255, 255, 0.12);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35);
  }
  .swipe-bg {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding-right: 20px;
    border-radius: inherit;
    background: var(--danger);
    color: #fff;
    opacity: 0;
  }
  .slot:has(.row.swiping) .swipe-bg {
    opacity: 1;
  }
  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 0;
    border-radius: inherit;
    background: transparent;
    touch-action: pan-y;
    transition: transform 0.2s ease;
  }
  .row.swiping {
    background: var(--player-dark);
    transition: none;
  }
  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 14px;
    text-align: left;
    color: inherit;
  }
  .pick img,
  .ph {
    width: 52px;
    height: 52px;
    flex-shrink: 0;
    border-radius: 4px;
    object-fit: cover;
    background: rgba(255, 255, 255, 0.1);
    -webkit-user-drag: none;
  }
  .meta {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .title,
  .artist {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .title {
    font-size: 17px;
    font-weight: 600;
  }
  .artist {
    font-size: 15px;
    color: rgba(255, 255, 255, 0.55);
  }
  .remove {
    display: none;
    width: 36px;
    height: 36px;
    color: rgba(255, 255, 255, 0.6);
  }
  @media (hover: hover) and (pointer: fine) {
    .row:hover .remove {
      display: inline-flex;
    }
  }
  .handle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    flex-shrink: 0;
    color: rgba(255, 255, 255, 0.45);
    cursor: grab;
    touch-action: none;
  }
  .list.dragging .handle {
    cursor: grabbing;
  }
  .empty {
    padding: 24px 0;
    color: rgba(255, 255, 255, 0.6);
    font-size: 15px;
  }
  .compact {
    height: auto;
    gap: 16px;
  }
  .compact .pill {
    height: 36px;
  }
  .compact .list {
    overflow: visible;
    -webkit-mask-image: none;
    mask-image: none;
    padding-bottom: 0;
  }
  .compact h3 {
    font-size: 16px;
  }
  .compact .sec-head p {
    font-size: 13px;
  }
  .compact .pick {
    gap: 12px;
  }
  .compact .pick img,
  .compact .ph {
    width: 44px;
    height: 44px;
  }
  .compact .title {
    font-size: 15px;
    font-weight: 500;
  }
  .compact .artist {
    font-size: 13px;
  }
  .compact .handle {
    width: 36px;
    height: 36px;
  }
  .compact .row.swiping {
    background: var(--bg-elevated);
  }
</style>
