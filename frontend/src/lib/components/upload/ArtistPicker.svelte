<script>
  // Исполнители релиза или трека в окне загрузки. Чипы — выбранные, первый
  // — основной. Поле: имя — подсказки (свои артисты kaikku, потом Deezer)
  // и в конце «Создать артиста»; ссылка на артиста Deezer или kaikku —
  // сразу этот артист.
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { lang } from '../../stores/settings.js';
  import { sized } from '../../imageproxy.js';
  import { fansLabel } from '../../format.js';
  import { artistIdFrom, isShortDeezerLink, isLocalId } from '../../links.js';
  import X from '@lucide/svelte/icons/x';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import UserRound from '@lucide/svelte/icons/user-round';
  import UserPlus from '@lucide/svelte/icons/user-plus';
  import Link from '@lucide/svelte/icons/link';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let artists = []; // [{ id, name, picture_url }] — основной первый
  export let query = ''; // текст в поле (окно подставляет имя из тегов файла)
  export let invalid = false; // подсветить: для публикации нужен хотя бы один
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

  // Ссылка (или голый ID) — не ищем по имени, а берём артиста по ней.
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

  async function create() {
    busy = true;
    try {
      add(await api.createArtist(name));
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
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
  function makeMain(i) {
    artists = [artists[i], ...artists.filter((_, j) => j !== i)];
    dispatch('change');
  }
</script>

<div class="picker" class:invalid>
  {#if artists.length}
    <ul class="chips">
      {#each artists as a, i (a.id)}
        <li class="chip">
          {#if a.picture_url}<img src={sized(a.picture_url, 64)} alt="" />{:else}<span class="ph"><UserRound size={14} /></span>{/if}
          <span class="cname">{a.name}</span>
          {#if i === 0 && artists.length > 1}<span class="main">{$t('edit.mainArtist')}</span>{/if}
          {#if i > 0}
            <button type="button" class="mini" title={$t('edit.makeMain')} aria-label={$t('edit.makeMain')} on:click={() => makeMain(i)}><ArrowUp size={14} /></button>
          {/if}
          <button type="button" class="mini" title={$t('upload.removeArtist')} aria-label={$t('upload.removeArtist')} on:click={() => remove(i)}><X size={14} /></button>
        </li>
      {/each}
    </ul>
  {/if}
  <div class="field">
    <input
      id={inputId}
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
            <!-- mousedown: раньше blur поля, иначе список закрылся бы до клика -->
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
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    height: 34px;
    padding: 0 4px 0 4px;
    border-radius: 999px;
    background: var(--bg-press);
  }
  .chip img,
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
    background: #444;
    color: var(--text-dim);
  }
  .ph.create {
    background: var(--text);
    color: var(--bg);
  }
  .cname {
    font-size: 14px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .main {
    font-size: 11px;
    color: var(--text-dim);
  }
  .mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .mini:hover {
      color: var(--text);
      background: #4a4a4a;
    }
  }
  .field {
    position: relative;
  }
  input {
    width: 100%;
    height: 40px;
    padding: 0 36px 0 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
  }
  input:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .invalid input {
    border-color: var(--danger);
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
    background: #2f2f2f;
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
    background: #3e3e3e;
  }
  @media (hover: hover) {
    .options button:hover {
      background: #3e3e3e;
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
