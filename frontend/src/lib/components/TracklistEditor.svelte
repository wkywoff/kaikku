<script>
  // Треклист релиза в окне правки альбома (админ): порядок — перетаскиванием
  // за ручку или стрелками, трек — заменить другим по ссылке Deezer или kaikku (не та
  // версия на релизе). Подставленный трек — настоящий трек Deezer со своим
  // ISRC, аудио и текстом (internal/api/tracklist.go). Сохраняет окно правки:
  // list — строки по порядку, list.map((r) => r.id) уходит на сервер.
  import { tick } from 'svelte';
  import { api } from '../api.js';
  import { dropRest, nextPaint, rowStep } from '../dragDrop.js';
  import { trackIdFrom, isShortDeezerLink } from '../links.js';
  import { t } from '../i18n.js';
  import AlignJustify from '@lucide/svelte/icons/align-justify';
  import Replace from '@lucide/svelte/icons/replace';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let list = []; // [{ id, title, artist, other }] — other: с другого релиза
  export let albumDeezerId;

  let replacing = -1; // строка, для которой открыто поле ссылки
  let link = '';
  let linkError = '';
  let linkBusy = false;

  function openReplace(i) {
    replacing = replacing === i ? -1 : i;
    link = '';
    linkError = '';
  }

  async function replace() {
    linkError = '';
    if (isShortDeezerLink(link)) {
      linkError = $t('edit.shortLink');
      return;
    }
    const id = trackIdFrom(link);
    if (!id) {
      linkError = $t('edit.badTrackLink');
      return;
    }
    if (list.some((r) => r.id === id)) {
      linkError = $t('edit.trackExists');
      return;
    }
    linkBusy = true;
    try {
      const tr = await api.discoverTrack(id);
      const i = replacing;
      list = list.map((r, j) => (j === i ? { id: String(tr.id), title: tr.title, artist: tr.artist, other: String(tr.album_id) !== String(albumDeezerId) } : r));
      replacing = -1;
    } catch (e) {
      linkError = e.message;
    } finally {
      linkBusy = false;
    }
  }

  function move(from, to) {
    if (from === to || to < 0 || to >= list.length) return;
    const next = list.slice();
    next.splice(to, 0, next.splice(from, 1)[0]);
    list = next;
    replacing = -1;
  }

  // --- перетаскивание за ручку (как в очереди, QueueView) ---
  let drag = null; // { i, y0, dy, h, id }
  function startDrag(e, i) {
    if (e.button !== 0) return;
    e.preventDefault();
    drag = { i, y0: e.clientY, dy: 0, h: rowStep(e.currentTarget.closest('.slot')), id: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function moveDrag(e) {
    if (drag && e.pointerId === drag.id) drag = { ...drag, dy: e.clientY - drag.y0 };
  }
  let settling = null; // { id, dy } — только что отпущенная строка доезжает на место (dragDrop.js)
  async function endDrag(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const d = { ...drag, dy: e.clientY - drag.y0 };
    const to = targetOf(d);
    const mine = (settling = { id: list[d.i].id, dy: dropRest(d, to) });
    drag = null;
    move(d.i, to);
    await tick();
    await nextPaint();
    if (settling === mine) settling = null;
  }
  const targetOf = (d) => (d ? Math.max(0, Math.min(list.length - 1, d.i + Math.round(d.dy / d.h))) : -1);
  $: target = targetOf(drag);
  function shift(i, d, to) {
    if (!d) return 0;
    if (i === d.i) return d.dy;
    if (d.i < to && i > d.i && i <= to) return -d.h;
    if (d.i > to && i < d.i && i >= to) return d.h;
    return 0;
  }
  function dragKey(e, i) {
    const dir = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    move(i, i + dir);
  }
</script>

<div class="tracklist" class:dragging={!!drag} class:settling={!!settling}>
  <span class="caption">{$t('edit.tracklist')}</span>
  {#each list as row, i (row.id)}
    <div
      class="slot"
      class:lifted={drag?.i === i || settling?.id === row.id}
      style="transform: translateY({drag ? shift(i, drag, target) : settling?.id === row.id ? settling.dy : 0}px)"
    >
      <div class="row">
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <span
          class="handle"
          role="button"
          tabindex="0"
          aria-label={$t('edit.moveTrack')}
          title={$t('edit.moveTrack')}
          on:pointerdown={(e) => startDrag(e, i)}
          on:pointermove={moveDrag}
          on:pointerup={endDrag}
          on:pointercancel={endDrag}
          on:keydown={(e) => dragKey(e, i)}><AlignJustify size={18} /></span
        >
        <span class="n">{i + 1}</span>
        <span class="meta">
          <span class="title">{row.title}</span>
          <span class="artist">{row.artist}{#if row.other}{' · '}<em>{$t('edit.otherRelease')}</em>{/if}</span>
        </span>
        <button type="button" class="icon-btn small" title={$t('edit.replaceTrack')} aria-label={$t('edit.replaceTrack')} class:on={replacing === i} on:click={() => openReplace(i)}><Replace size={16} /></button>
      </div>
      {#if replacing === i}
        <div class="replace">
          <!-- svelte-ignore a11y_autofocus -->
          <input type="text" bind:value={link} placeholder={$t('edit.trackLink')} autofocus on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), replace())} />
          <button type="button" class="add" disabled={!link.trim() || linkBusy} on:click={replace}>
            {#if linkBusy}<LoaderCircle class="spin" size={16} />{/if}
            {$t('edit.replace')}
          </button>
        </div>
        {#if linkError}<p class="error-text">{linkError}</p>{/if}
      {/if}
    </div>
  {/each}
</div>

<style>
  .tracklist {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-top: 20px;
  }
  .caption {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
    margin-bottom: 4px;
  }
  .slot {
    position: relative;
    border-radius: 6px;
    background: #282828;
    transition: transform 0.15s ease;
  }
  .dragging .slot.lifted,
  .settling .slot {
    transition: none;
  }
  .slot.lifted {
    z-index: 1;
  }
  .lifted .row {
    background: #3a3a3a;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 48px;
    padding-right: 4px;
    border-radius: 6px;
    background: #333;
  }
  .handle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 44px;
    flex-shrink: 0;
    color: var(--text-dim);
    cursor: grab;
    touch-action: none; /* тащить, а не прокручивать */
  }
  .dragging .handle {
    cursor: grabbing;
  }
  .n {
    width: 22px;
    flex-shrink: 0;
    text-align: right;
    font-size: 14px;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    padding-left: 8px;
  }
  .title,
  .artist {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-size: 15px;
  }
  .artist {
    font-size: 13px;
    color: var(--text-dim);
  }
  .artist em {
    font-style: normal;
    color: var(--accent);
  }
  .icon-btn.on {
    color: var(--text);
    background: #4a4a4a;
  }
  .replace {
    display: flex;
    gap: 8px;
    padding: 8px 4px 4px;
  }
  .replace input {
    flex: 1;
    min-width: 0;
    height: 40px;
    padding: 0 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
  }
  .replace input:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .add {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 40px;
    padding: 0 16px;
    border-radius: 999px;
    border: 1px solid #6a6a6a;
    font-size: 14px;
    font-weight: 600;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .add:hover:not(:disabled) {
      border-color: #fff;
    }
  }
  .error-text {
    margin: 4px 4px 6px;
  }
</style>
