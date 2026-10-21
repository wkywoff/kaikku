<script>
  // Треки в окне загрузки: зона для файлов, строки по порядку (перетащить
  // за ручку или стрелками — как в TracklistEditor), у каждой — название,
  // метка E, длительность и состояние загрузки; в меню — исполнители и
  // текст трека, другой файл, удалить.
  // Строка: { key, id (k… — когда файл обработан), title, durationMs,
  //   explicit, artists (null — как у релиза), state, progress, error,
  //   lyrics (undefined — ещё не загружен), lyricsInitial, open, busy (меняется файл),
  //   other (трек с другого релиза: только переставить и убрать) }.
  import { createEventDispatcher, tick } from 'svelte';
  import { dropRest, nextPaint, rowStep } from '../../dragDrop.js';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { formatTime } from '../../stores/player.js';
  import { openMenu } from '../../stores/contextMenu.js';
  import { AUDIO_ACCEPT, isAudioFile, naturalCompare } from '../../uploads.js';
  import ArtistPicker from './ArtistPicker.svelte';
  import AlignJustify from '@lucide/svelte/icons/align-justify';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import Upload from '@lucide/svelte/icons/upload';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import UsersRound from '@lucide/svelte/icons/users-round';
  import FileAudio from '@lucide/svelte/icons/file-audio';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import X from '@lucide/svelte/icons/x';
  import Link2 from '@lucide/svelte/icons/link-2';

  export let rows = [];
  export let releaseArtists = [];
  export let invalidTitles = false; // подсветить пустые названия (не хватает для публикации)
  export let invalid = false; // нет ни одного трека

  const dispatch = createEventDispatcher();
  let fileInput;
  let replaceInput;
  let replaceRow = null;
  let over = false;

  function pick(list) {
    const files = [...list].filter(isAudioFile).sort((a, b) => naturalCompare(a.name, b.name));
    if (files.length) dispatch('files', files);
    else if (list.length) dispatch('rejected');
  }
  function onDrop(e) {
    over = false;
    pick(e.dataTransfer?.files || []);
  }

  function menu(e, row) {
    if (row.other) return openMenu(e, [{ label: $t('upload.removeFromRelease'), icon: X, onClick: () => dispatch('remove', row) }]);
    openMenu(e, [
      ...(row.id ? [{ label: $t('upload.trackDetails'), icon: UsersRound, onClick: () => toggleDetails(row) }] : []),
      ...(row.id
        ? [{ label: $t('upload.replaceAudio'), icon: FileAudio, onClick: () => ((replaceRow = row), replaceInput.click()) }]
        : []),
      { label: $t('upload.removeTrack'), icon: Trash2, danger: true, onClick: () => dispatch('remove', row) },
    ]);
  }

  async function toggleDetails(row) {
    row.open = !row.open;
    rows = rows;
    if (row.open && row.id && row.lyrics === undefined) {
      try {
        const { text } = await api.trackLyricsText(row.id);
        row.lyrics = row.lyricsInitial = text;
      } catch {
        row.lyrics = row.lyricsInitial = '';
      }
      rows = rows;
    }
  }

  function ownArtists(row, on) {
    row.artists = on ? releaseArtists.map((a) => ({ ...a })) : null;
    changed();
  }

  const changed = () => ((rows = rows), dispatch('change'));
  const artistNames = (row) => (row.artists || releaseArtists).map((a) => a.name).join(', ');

  // --- порядок: перетаскивание за ручку (как в очереди и TracklistEditor) ---
  function move(from, to) {
    if (from === to || to < 0 || to >= rows.length) return;
    const next = rows.slice();
    next.splice(to, 0, next.splice(from, 1)[0]);
    rows = next;
    dispatch('change');
  }
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
  let settling = null; // { key, dy } — только что отпущенная строка доезжает на место (dragDrop.js)
  async function endDrag(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const d = { ...drag, dy: e.clientY - drag.y0 };
    const to = targetOf(d);
    const mine = (settling = { key: rows[d.i].key, dy: dropRest(d, to) });
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

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="drop"
  class:over
  class:invalid={invalid && !rows.length}
  on:dragover|preventDefault={() => (over = true)}
  on:dragleave={() => (over = false)}
  on:drop|preventDefault={onDrop}
>
  <Upload size={28} />
  <div class="drop-text">
    <strong>{$t('upload.dropAudio')}</strong>
    <span>{$t('upload.audioHint')}</span>
  </div>
  <button type="button" class="pill outline" on:click={() => fileInput.click()}>{$t('upload.chooseFiles')}</button>
  <input bind:this={fileInput} type="file" accept={AUDIO_ACCEPT} multiple hidden on:change={(e) => (pick(e.currentTarget.files), (e.currentTarget.value = ''))} />
  <input
    bind:this={replaceInput}
    type="file"
    accept={AUDIO_ACCEPT}
    hidden
    on:change={(e) => {
      const f = e.currentTarget.files[0];
      e.currentTarget.value = '';
      if (f && replaceRow) dispatch('replace', { row: replaceRow, file: f });
    }}
  />
</div>

{#if rows.length}
  <div class="rows" class:dragging={!!drag} class:settling={!!settling}>
    {#each rows as row, i (row.key)}
      <!-- Сдвиг — только пока тащат: transform у строки поднимает её в свой
           слой, и подсказки исполнителей уходили под следующие строки. -->
      <div
        class="slot"
        class:lifted={drag?.i === i || settling?.key === row.key}
        class:open={row.open}
        style:transform={drag ? `translateY(${shift(i, drag, target)}px)` : settling?.key === row.key ? `translateY(${settling.dy}px)` : null}
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
          <span class="n num">{i + 1}</span>
          <div class="main">
            {#if row.other}
              <span class="title fixed">{row.title}</span>
              <span class="who">{artistNames(row)}{' · '}<em>{$t('edit.otherRelease')}</em></span>
            {:else}
              <input
                class="title"
                class:missing={invalidTitles && row.id && !row.title.trim()}
                type="text"
                maxlength="300"
                placeholder={$t('upload.trackTitle')}
                aria-label={$t('upload.trackTitleN', { n: i + 1 })}
                bind:value={row.title}
                on:input={() => dispatch('change')}
              />
              {#if row.id}
                <button type="button" class="who" on:click={() => toggleDetails(row)}>
                  {row.artists ? artistNames(row) : $t('upload.asRelease', { names: artistNames(row) || '—' })}
                </button>
              {:else if row.state === 'error'}
                <span class="status err"><CircleAlert size={14} /> {row.error || $t('upload.failed')}</span>
              {:else}
                <span class="status">
                  {#if row.state === 'uploading'}
                    <span class="bar"><span style:width="{Math.round(row.progress * 100)}%"></span></span>
                    {$t('upload.uploadingPct', { pct: Math.round(row.progress * 100) })}
                  {:else if row.state === 'processing'}
                    <LoaderCircle class="spin" size={14} /> {$t('upload.processing')}
                  {:else}
                    {$t('upload.queued')}
                  {/if}
                </span>
              {/if}
            {/if}
          </div>
          {#if row.other}
            <!-- те же колонки, что у своих: E, длительность, значок -->
            <span class="e on" class:ghost={!row.explicit} title={row.explicit ? $t('upload.explicitHint') : null}>E</span>
            <span class="dur num">{formatTime((row.durationMs || 0) / 1000)}</span>
            <span class="ok linked" title={$t('edit.otherRelease')}><Link2 size={16} /></span>
          {:else if row.id}
            <button
              type="button"
              class="e"
              class:on={row.explicit}
              aria-pressed={row.explicit}
              title={$t('upload.explicitHint')}
              on:click={() => ((row.explicit = !row.explicit), changed())}>E</button
            >
            <span class="dur num">{formatTime((row.durationMs || 0) / 1000)}</span>
            {#if row.busy}
              <span class="ok busy" title={$t('upload.processing')}><LoaderCircle class="spin" size={16} /></span>
            {:else}
              <span class="ok" title={$t('upload.ready')}><CircleCheck size={16} /></span>
            {/if}
          {:else if row.state === 'error'}
            <button type="button" class="icon-btn" title={$t('upload.retry')} aria-label={$t('upload.retry')} on:click={() => dispatch('retry', row)}>
              <RotateCw size={18} />
            </button>
          {/if}
          <button type="button" class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: row.title })} on:click={(e) => menu(e, row)}>
            <Ellipsis size={20} />
          </button>
        </div>
        {#if row.open && row.id && !row.other}
          <div class="details">
            <label class="check">
              <input type="checkbox" checked={!!row.artists} on:change={(e) => ownArtists(row, e.currentTarget.checked)} />
              <span>{$t('upload.ownTrackArtists')}<small>{$t('upload.ownTrackArtistsHint')}</small></span>
            </label>
            {#if row.artists}
              <ArtistPicker bind:artists={row.artists} inputId="track-artists-{row.key}" label={$t('upload.trackArtists')} on:change={changed} />
            {/if}
            <label class="lyrics">
              <span>{$t('upload.lyrics')}<small>{$t('upload.lyricsHint')}</small></span>
              {#if row.lyrics === undefined}
                <LoaderCircle class="spin" size={16} />
              {:else}
                <textarea rows="6" maxlength="60000" bind:value={row.lyrics} on:input={() => dispatch('change')}></textarea>
              {/if}
            </label>
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .drop {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 20px;
    border: 2px dashed #4a4a4a;
    border-radius: 8px;
    color: var(--text-dim);
    transition:
      border-color 0.15s,
      background-color 0.15s;
  }
  .drop.over {
    border-color: var(--text);
    background: rgba(255, 255, 255, 0.04);
  }
  .drop.invalid {
    border-color: var(--danger);
  }
  .drop-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .drop-text strong {
    color: var(--text);
    font-size: 15px;
  }
  .drop-text span {
    font-size: 13px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 12px;
  }
  .slot {
    position: relative;
    border-radius: 6px;
    background: #282828;
    transition: transform 0.15s ease;
  }
  .slot.open {
    z-index: 2; /* подсказки исполнителей — поверх следующих строк */
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
    min-height: 60px;
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
    padding-left: 8px;
  }
  .title {
    height: 34px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
  }
  @media (hover: hover) {
    .title:hover {
      background: #3a3a3a;
    }
  }
  .title:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .title.missing {
    border-color: var(--danger);
  }
  .title.fixed {
    display: block;
    line-height: 34px;
    border: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .title.fixed:hover {
      background: transparent;
    }
  }
  .who em {
    font-style: normal;
    color: var(--accent);
  }
  .who,
  .status {
    display: inline-flex;
    line-height: 17px; /* у кнопки и у подписи — одна высота: строки равные, шаг перетаскивания ровный */
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    font-size: 13px;
    color: var(--text-dim);
    text-align: left;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (hover: hover) {
    .who:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .status.err {
    color: var(--danger);
    white-space: normal;
  }
  .bar {
    width: 120px;
    height: 4px;
    border-radius: 2px;
    background: #4a4a4a;
    overflow: hidden;
    flex-shrink: 0;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--text);
    transition: width 0.2s;
  }
  .e {
    width: 26px;
    height: 22px;
    border-radius: 3px;
    border: 1px solid #5a5a5a;
    color: var(--text-faint);
    font-size: 12px;
    font-weight: 700;
    flex-shrink: 0;
  }
  span.e {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .e.on {
    background: var(--text-dim);
    border-color: var(--text-dim);
    color: var(--bg);
  }
  .dur {
    width: 44px;
    text-align: right;
    color: var(--text-dim);
    font-size: 14px;
    flex-shrink: 0;
  }
  .ok {
    display: inline-flex;
    color: var(--ok);
    flex-shrink: 0;
  }
  .ok.busy,
  .ok.linked {
    color: var(--text-dim);
  }
  .e.ghost {
    visibility: hidden;
  }
  .details {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 12px 12px 16px 70px;
  }
  .check {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    font-size: 14px;
    cursor: pointer;
  }
  .check input {
    margin-top: 3px;
  }
  small {
    display: block;
    color: var(--text-dim);
    font-size: 12px;
    margin-top: 2px;
  }
  .lyrics {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 14px;
  }
  textarea {
    width: 100%;
    padding: 8px 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font: 14px/1.5 var(--font);
    resize: vertical;
  }
  textarea:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  @media (max-width: 560px) {
    .drop {
      flex-direction: column;
      align-items: flex-start;
    }
    .details {
      padding-left: 12px;
    }
    .dur {
      display: none;
    }
    .bar {
      width: 64px;
    }
  }
</style>
