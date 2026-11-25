<script context="module">
  export function rowFromTrack(tr, album) {
    const names = tr.contributors?.length ? tr.contributors.map((a) => a.name) : [tr.artist];
    return {
      id: String(tr.id),
      title: tr.title,
      artist: names.filter(Boolean).join(', '),
      other: String(tr.album_id) !== String(album),
      durationMs: tr.duration_ms || 0,
      explicit: !!tr.explicit,
    };
  }
</script>

<script>
  import { createEventDispatcher, tick } from 'svelte';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { formatTime } from '../../stores/player.js';
  import { openMenu } from '../../stores/contextMenu.js';
  import { dropRest, nextPaint, rowStep } from '../../dragDrop.js';
  import { trackIdFrom, isShortDeezerLink, isLocalId } from '../../links.js';
  import AlignJustify from '@lucide/svelte/icons/align-justify';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Replace from '@lucide/svelte/icons/replace';
  import X from '@lucide/svelte/icons/x';
  import Link2 from '@lucide/svelte/icons/link-2';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let rows = [];
  export let albumId;

  const dispatch = createEventDispatcher();

  let replacing = -1;
  let link = '';
  let linkError = '';
  let busy = '';
  let addLink = '';
  let addError = '';

  async function trackByLink(text) {
    if (isShortDeezerLink(text)) throw new Error($t('edit.shortLink'));
    const id = trackIdFrom(text);
    if (!id) throw new Error($t('edit.badTrackLink'));
    if (isLocalId(id)) throw new Error($t('edit.deezerOnly'));
    if (rows.some((r) => r.id === id)) throw new Error($t('edit.trackExists'));
    return rowFromTrack(await api.discoverTrack(id), albumId);
  }

  async function replace() {
    linkError = '';
    busy = 'replace';
    try {
      const row = await trackByLink(link);
      const i = replacing;
      rows = rows.map((r, j) => (j === i ? row : r));
      replacing = -1;
      dispatch('change');
    } catch (e) {
      linkError = e.message;
    } finally {
      busy = '';
    }
  }

  async function add() {
    addError = '';
    busy = 'add';
    try {
      rows = [...rows, await trackByLink(addLink)];
      addLink = '';
      dispatch('change');
    } catch (e) {
      addError = e.message;
    } finally {
      busy = '';
    }
  }

  function menu(e, row, i) {
    openMenu(e, [
      { label: $t('edit.trackTitle'), icon: Pencil, onClick: () => dispatch('edit', row) },
      {
        label: $t('edit.replaceTrack'),
        icon: Replace,
        onClick: () => ((replacing = i), (link = ''), (linkError = '')),
      },
      ...(rows.length > 1
        ? [{ label: $t('upload.removeFromRelease'), icon: X, onClick: () => ((rows = rows.filter((r) => r !== row)), (replacing = -1), dispatch('change')) }]
        : []),
    ]);
  }

  function move(from, to) {
    if (from === to || to < 0 || to >= rows.length) return;
    const next = rows.slice();
    next.splice(to, 0, next.splice(from, 1)[0]);
    rows = next;
    replacing = -1;
    dispatch('change');
  }
  let drag = null;
  function startDrag(e, i) {
    if (e.button !== 0) return;
    e.preventDefault();
    drag = { i, y0: e.clientY, dy: 0, h: rowStep(e.currentTarget.closest('.slot')), id: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function moveDrag(e) {
    if (drag && e.pointerId === drag.id) drag = { ...drag, dy: e.clientY - drag.y0 };
  }
  let settling = null;
  async function endDrag(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const d = { ...drag, dy: e.clientY - drag.y0 };
    const to = targetOf(d);
    const mine = (settling = { id: rows[d.i].id, dy: dropRest(d, to) });
    drag = null;
    move(d.i, to);
    await tick();
    await nextPaint();
    if (settling === mine) settling = null;
  }
  const targetOf = (d) => (d ? Math.max(0, Math.min(rows.length - 1, d.i + Math.round(d.dy / d.h))) : -1);
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

<div class="rows" class:dragging={!!drag} class:settling={!!settling}>
  {#each rows as row, i (row.id)}
    <div
      class="slot"
      class:lifted={drag?.i === i || settling?.id === row.id}
      style:transform={drag ? `translateY(${shift(i, drag, target)}px)` : settling?.id === row.id ? `translateY(${settling.dy}px)` : null}
    >
      <div class="row">
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
        <span class="n num">{i + 1}</span>
        <div class="main">
          <span class="title">{row.title}</span>
          <span class="who">{row.artist}{#if row.other}{' • '}<em>{$t('edit.otherRelease')}</em>{/if}</span>
        </div>
        <span class="e" class:ghost={!row.explicit} title={row.explicit ? $t('upload.explicitHint') : null}>E</span>
        <span class="dur num">{formatTime(row.durationMs / 1000)}</span>
        <button type="button" class="icon-btn pencil" title={$t('edit.trackTitle')} aria-label={$t('edit.trackTitle')} on:click={() => dispatch('edit', row)}>
          <Pencil size={18} />
        </button>
        <button type="button" class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: row.title })} on:click={(e) => menu(e, row, i)}>
          <Ellipsis size={20} />
        </button>
      </div>
      {#if replacing === i}
        <div class="replace field">
          <div class="link-row">
            <!-- svelte-ignore a11y_autofocus -->
            <input type="text" bind:value={link} placeholder={$t('edit.trackLink')} aria-label={$t('edit.replaceTrack')} autofocus
              on:keydown={(e) => (e.key === 'Enter' && (e.preventDefault(), replace())) || (e.key === 'Escape' && (replacing = -1))} />
            <button type="button" class="pill outline" disabled={!link.trim() || !!busy} on:click={replace}>
              {#if busy === 'replace'}<LoaderCircle class="spin" size={16} />{/if}{$t('edit.replace')}
            </button>
            <button type="button" class="icon-btn" title={$t('album.close')} aria-label={$t('album.close')} on:click={() => (replacing = -1)}><X size={18} /></button>
          </div>
          {#if linkError}<p class="error-text">{linkError}</p>{:else}<small>{$t('edit.swapHint')}</small>{/if}
        </div>
      {/if}
    </div>
  {/each}
</div>

<div class="field add">
  <label class="label sub" for="add-track"><Link2 size={16} /> {$t('edit.addTrack')}</label>
  <div class="link-row">
    <input id="add-track" type="text" bind:value={addLink} placeholder={$t('edit.trackLink')}
      on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), add())} />
    <button type="button" class="pill outline" disabled={!addLink.trim() || !!busy} on:click={add}>
      {#if busy === 'add'}<LoaderCircle class="spin" size={16} />{/if}{$t('upload.add')}
    </button>
  </div>
  {#if addError}<p class="error-text">{addError}</p>{/if}
  <small>{$t('edit.addTrackHint')}</small>
</div>

<style>
  .rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
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
    z-index: 3;
  }
  .lifted .row {
    background: #3a3a3a;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 62px;
    padding: 4px 4px 4px 0;
    border-radius: 6px;
    background: #2e2e2e;
  }
  .handle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 48px;
    flex-shrink: 0;
    color: var(--text-dim);
    cursor: grab;
    touch-action: none;
  }
  .dragging .handle {
    cursor: grabbing;
  }
  .n {
    width: 22px;
    flex-shrink: 0;
    text-align: right;
    color: var(--text-dim);
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-left: 16px;
  }
  .title,
  .who {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-size: 15px;
  }
  .who {
    font-size: 13px;
    color: var(--text-dim);
  }
  .who em {
    font-style: normal;
    color: var(--accent);
  }
  .e {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 22px;
    border-radius: 3px;
    background: var(--text-dim);
    color: var(--bg);
    font-size: 12px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .e.ghost {
    visibility: hidden;
  }
  .dur {
    width: 44px;
    text-align: right;
    color: var(--text-dim);
    font-size: 14px;
    flex-shrink: 0;
    margin-right: 4px;
  }
  .replace {
    padding: 10px 8px 12px 68px;
  }
  .add {
    margin-top: 0;
    padding-top: 20px;
    border-top: 1px solid var(--border);
  }
  .error-text {
    margin: 0;
  }
  @media (max-width: 560px) {
    .dur,
    .pencil,
    .e.ghost {
      display: none;
    }
    .replace {
      padding-left: 8px;
    }
  }
</style>
