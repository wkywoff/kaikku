<script>
  import { createEventDispatcher, onDestroy, tick } from 'svelte';
  import { dropRest, nextPaint, rowStep } from '../../dragDrop.js';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { lang } from '../../stores/settings.js';
  import { sized } from '../../imageproxy.js';
  import { fansLabel } from '../../format.js';
  import { artistIdFrom, isShortDeezerLink, isLocalId } from '../../links.js';
  import { newArtist, isNewArtist } from '../../uploads.js';
  import X from '@lucide/svelte/icons/x';
  import AlignJustify from '@lucide/svelte/icons/align-justify';
  import UserRound from '@lucide/svelte/icons/user-round';
  import UserPlus from '@lucide/svelte/icons/user-plus';
  import Link from '@lucide/svelte/icons/link';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let artists = [];
  export let query = '';
  export let invalid = false;
  export let label = '';
  export let inputId = 'artist-picker';

  const dispatch = createEventDispatcher();
  const listId = `${inputId}-list`;

  let results = [];
  let open = false;
  let active = -1;
  let busy = false;
  let error = '';
  let timer;
  let controller;

  $: linkId = /\/|^\d+$|^k\d+$/.test(query.trim()) ? artistIdFrom(query) : null;
  $: looksLikeLink = /\/|deezer|^\d+$/.test(query.trim());
  $: name = query.trim();
  $: options = [
    ...(linkId ? [{ kind: 'link' }] : results.map((a) => ({ kind: 'artist', artist: a }))),
    ...(name && !looksLikeLink ? [{ kind: 'create' }] : []),
  ];
  $: search(query);

  function search(q) {
    clearTimeout(timer);
    controller?.abort();
    error = '';
    active = -1;
    const text = q.trim();
    if (text.length < 2 || /\/|deezer|^\d+$/.test(text)) {
      results = [];
      return;
    }
    timer = setTimeout(async () => {
      controller = new AbortController();
      try {
        const { artists: found } = await api.searchArtists(text, { signal: controller.signal });
        if (q === query) results = found.filter((a) => !artists.some((x) => x.id === String(a.id))).slice(0, 8);
      } catch (e) {
        if (e.name !== 'AbortError') results = [];
      }
    }, 250);
  }
  onDestroy(() => {
    clearTimeout(timer);
    controller?.abort();
  });

  function add(a) {
    const id = String(a.id);
    if (artists.some((x) => x.id === id)) {
      error = $t('edit.artistExists');
      return;
    }
    artists = [...artists, { id, name: a.name, picture_url: a.picture_url || null }];
    query = '';
    results = [];
    open = false;
    dispatch('change');
  }

  async function addByLink() {
    if (isShortDeezerLink(query)) {
      error = $t('edit.shortLink');
      return;
    }
    if (!linkId) {
      error = $t('edit.badArtistLink');
      return;
    }
    busy = true;
    try {
      add(await api.artistInfo(linkId));
    } catch (e) {
      error = e.status === 404 ? $t('upload.noSuchArtist') : e.message;
    } finally {
      busy = false;
    }
  }

  function create() {
    add(newArtist(name));
  }

  function choose(opt) {
    if (opt.kind === 'link') addByLink();
    else if (opt.kind === 'create') create();
    else add(opt.artist);
  }

  function onKey(e) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (!options.length) return;
      e.preventDefault();
      open = true;
      active = (active + (e.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (active >= 0 && options[active]) choose(options[active]);
      else if (linkId || looksLikeLink) addByLink();
      else if (options.length === 1) choose(options[0]);
    } else if (e.key === 'Escape' && open) {
      e.stopPropagation();
      open = false;
    }
  }

  function remove(i) {
    artists = artists.filter((_, j) => j !== i);
    dispatch('change');
  }

  function move(from, to) {
    if (from === to || to < 0 || to >= artists.length) return;
    const next = artists.slice();
    next.splice(to, 0, next.splice(from, 1)[0]);
    artists = next;
    dispatch('change');
  }
  let drag = null;
  function startDrag(e, i) {
    if (e.button !== 0 || artists.length < 2) return;
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
    const mine = (settling = { id: artists[d.i].id, dy: dropRest(d, to) });
    drag = null;
    move(d.i, to);
    await tick();
    await nextPaint();
    if (settling === mine) settling = null;
  }
  const targetOf = (d) => (d ? Math.max(0, Math.min(artists.length - 1, d.i + Math.round(d.dy / d.h))) : -1);
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
  const sourceOf = (a) => (isNewArtist(a) || isLocalId(a.id) ? 'kaikku' : 'Deezer');
  $: mixed = new Set(artists.map(sourceOf)).size > 1;
  $: noteOf = (a) => (isNewArtist(a) ? $t('upload.newArtistHint') : mixed ? sourceOf(a) : '');
</script>

<div class="picker" class:invalid>
  {#if artists.length}
    <ul class="rows" class:dragging={!!drag} class:settling={!!settling} class:single={artists.length < 2}>
      {#each artists as a, i (a.id)}
        <li
          class="slot"
          class:lifted={drag?.i === i || settling?.id === a.id}
          style:transform={drag ? `translateY(${shift(i, drag, target)}px)` : settling?.id === a.id ? `translateY(${settling.dy}px)` : null}
        >
          <div class="row">
            <span
              class="handle"
              role="button"
              tabindex={artists.length > 1 ? 0 : -1}
              aria-label={$t('edit.moveArtist')}
              title={artists.length > 1 ? $t('edit.moveArtist') : null}
              on:pointerdown={(e) => startDrag(e, i)}
              on:pointermove={moveDrag}
              on:pointerup={endDrag}
              on:pointercancel={endDrag}
              on:keydown={(e) => dragKey(e, i)}><AlignJustify size={18} /></span
            >
            {#if a.picture_url}<img src={sized(a.picture_url, 64)} alt="" />{:else}<span class="ph"><UserRound size={16} /></span>{/if}
            <span class="who">
              <span class="cname">{a.name}</span>
              {#if noteOf(a)}<span class="role">{noteOf(a)}</span>{/if}
            </span>
            <button type="button" class="icon-btn" title={$t('upload.removeArtist')} aria-label={$t('upload.removeArtist')} on:click={() => remove(i)}><X size={18} /></button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
  <div class="field">
    <input
      id={inputId}
      class="input"
      class:invalid
      type="text"
      autocomplete="off"
      role="combobox"
      aria-expanded={open && options.length > 0}
      aria-controls={listId}
      aria-label={label || $t('upload.artists')}
      placeholder={artists.length ? $t('upload.addArtist') : $t('upload.artistPlaceholder')}
      bind:value={query}
      on:input={() => (open = true)}
      on:focus={() => (open = true)}
      on:blur={() => setTimeout(() => (open = false), 150)}
      on:keydown={onKey}
    />
    {#if busy}<span class="busy"><LoaderCircle class="spin" size={16} /></span>{/if}
    {#if open && options.length}
      <ul class="options" id={listId} role="listbox">
        {#each options as opt, i}
          <li role="option" aria-selected={i === active} class:active={i === active}>
            <button type="button" on:mousedown|preventDefault={() => choose(opt)}>
              {#if opt.kind === 'artist'}
                {#if opt.artist.picture_url}<img src={sized(opt.artist.picture_url, 64)} alt="" />{:else}<span class="ph"><UserRound size={14} /></span>{/if}
                <span class="oname">{opt.artist.name}</span>
                <span class="src">{isLocalId(opt.artist.id) ? 'kaikku' : fansLabel(opt.artist.fans, $lang) || 'Deezer'}</span>
              {:else if opt.kind === 'link'}
                <span class="ph"><Link size={14} /></span>
                <span class="oname">{$t('upload.addByLink')}</span>
              {:else}
                <span class="ph create"><UserPlus size={14} /></span>
                <span class="oname">{$t('upload.createArtist', { name })}</span>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  {#if error}<p class="error-text">{error}</p>{/if}
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .slot {
    position: relative;
    border-radius: 6px;
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
    background: var(--raised-hover);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 52px;
    padding: 4px 4px 4px 0;
    border-radius: 6px;
    background: var(--raised);
  }
  .handle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 44px;
    margin-right: -4px;
    flex-shrink: 0;
    color: var(--text-dim);
    cursor: grab;
    touch-action: none;
  }
  .dragging .handle {
    cursor: grabbing;
  }
  .single .handle {
    cursor: default;
    opacity: 0.35;
  }
  .row img,
  .row .ph {
    width: 36px;
    height: 36px;
  }
  .who {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .role {
    font-size: 12px;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  img,
  .ph {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
  }
  .ph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--field);
    color: var(--text-dim);
  }
  .ph.create {
    background: var(--text);
    color: var(--bg);
  }
  .cname {
    font-size: 15px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .field {
    position: relative;
  }
  .field .input {
    padding-right: 36px;
  }
  .busy {
    position: absolute;
    right: 10px;
    top: 12px;
    color: var(--text-dim);
  }
  .options {
    position: absolute;
    z-index: 5;
    left: 0;
    right: 0;
    top: calc(100% + 4px);
    margin: 0;
    padding: 4px;
    list-style: none;
    border-radius: 6px;
    background: var(--raised);
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
    max-height: 320px;
    overflow-y: auto;
  }
  .options button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 44px;
    padding: 6px 8px;
    border-radius: 4px;
    text-align: left;
  }
  .options li.active button {
    background: var(--field);
  }
  @media (hover: hover) {
    .options button:hover {
      background: var(--field);
    }
  }
  .options img {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    object-fit: cover;
  }
  .options .ph {
    width: 32px;
    height: 32px;
  }
  .oname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
  }
  .src {
    font-size: 12px;
    color: var(--text-dim);
    white-space: nowrap;
  }
  .error-text {
    margin: 0;
  }
</style>
