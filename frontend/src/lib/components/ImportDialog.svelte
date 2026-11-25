<script>
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { refreshPlaylists } from '../stores/collection.js';
  import { parseTrackList, spotifyLink } from '../importList.js';
  import Dialog from './Dialog.svelte';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import FileUp from '@lucide/svelte/icons/file-up';

  const dispatch = createEventDispatcher();
  const BATCH = 20;
  const ADD_CHUNK = 50;

  let step = 'input';
  let text = '';
  let error = '';
  let busy = false;
  let source = null;
  let tracks = [];
  let matches = [];
  let done = 0;
  let title = '';
  let cancelled = false;
  let fileInput;
  let textarea;

  $: found = matches.filter(Boolean);
  $: missing = tracks.filter((_, i) => matches[i] === null);

  onMount(() => {
    textarea?.focus();
  });
  onDestroy(() => (cancelled = true));

  function close() {
    cancelled = true;
    dispatch('close');
  }

  async function readFile(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (file) text = await file.text();
  }

  async function start() {
    error = '';
    busy = true;
    created = null;
    added = 0;
    try {
      const link = spotifyLink(text);
      if (link) {
        const list = await api.importSpotify(link);
        source = { title: list.title, spotify: true, truncated: list.truncated };
        tracks = list.tracks;
      } else {
        source = { title: '', spotify: false, truncated: false };
        tracks = parseTrackList(text);
      }
      if (!tracks.length) {
        error = $t('import.nothing');
        return;
      }
      title = source.title || $t('import.defaultTitle');
      await match();
    } catch (e) {
      error = e.message;
      step = 'input';
    } finally {
      busy = false;
    }
  }

  async function match() {
    step = 'matching';
    matches = [];
    done = 0;
    for (let i = 0; i < tracks.length && !cancelled; i += BATCH) {
      const batch = tracks.slice(i, i + BATCH);
      matches = [...matches, ...(await matchBatch(batch))];
      done = Math.min(tracks.length, i + BATCH);
    }
    if (cancelled) return;
    if (matches.filter(Boolean).length > 1) {
      step = 'settling';
      matches = await settleAlbums(matches);
    }
    if (!cancelled) step = 'done';
  }

  async function settleAlbums(list) {
    try {
      const settled = (await api.importAlbums(list)).matches;
      return settled?.length === list.length ? settled : list;
    } catch {
      return list;
    }
  }

  async function matchBatch(batch) {
    for (let attempt = 0; attempt < 3 && !cancelled; attempt++) {
      try {
        return (await api.importMatch(batch)).matches;
      } catch {
        await new Promise((r) => setTimeout(r, 3000 * (attempt + 1)));
      }
    }
    return batch.map(() => null);
  }

  let created = null;
  let added = 0;

  async function create() {
    busy = true;
    error = '';
    try {
      const ids = [...new Set(found.map((m) => m.id))];
      created ||= await api.createPlaylist({
        title: title.trim() || $t('import.defaultTitle'),
        description: source.spotify ? $t('import.fromSpotify') : '',
      });
      while (added < ids.length) {
        const chunk = ids.slice(added, added + ADD_CHUNK);
        await api.addToPlaylist(created.id, chunk, { download: false });
        added += chunk.length;
      }
      await refreshPlaylists();
      dispatch('created', created);
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  const label = (tr) => [tr.artists?.join(', '), tr.title].filter(Boolean).join(' — ');
</script>

<Dialog title={$t('import.title')} locked={busy && step !== 'matching' && step !== 'settling'} keepOnBackdrop={busy} on:close={close}>
  {#if step === 'input'}
    <p class="hint">{$t('import.hint')}</p>
    <textarea class="input" bind:this={textarea} bind:value={text} rows="7" placeholder={$t('import.placeholder')} disabled={busy}></textarea>
    <div class="row">
      <button type="button" class="text-btn" on:click={() => fileInput.click()} disabled={busy}><FileUp size={16} /> {$t('import.csv')}</button>
      <input bind:this={fileInput} type="file" accept=".csv,.txt,text/csv,text/plain" hidden on:change={readFile} />
      <div class="spacer"></div>
      <button type="button" class="pill primary lg" on:click={start} disabled={busy || !text.trim()}>
        {#if busy}<LoaderCircle class="spin" size={16} />{/if}
        {$t('import.find')}
      </button>
    </div>
  {:else if step === 'matching'}
    <p>{$t('import.matching', { done, total: tracks.length })}</p>
    <div class="bar"><div style="width: {(done / tracks.length) * 100}%"></div></div>
  {:else if step === 'settling'}
    <p>{$t('import.settling')}</p>
    <div class="bar"><div style="width: 100%"></div></div>
  {:else}
    <p class="summary">{$t('import.found', { found: found.length, total: tracks.length })}</p>
    {#if source.truncated}<p class="hint warn">{$t('import.truncated')}</p>{/if}
    <label>
      <span>{$t('playlist.name')}</span>
      <input class="input" type="text" bind:value={title} maxlength="300" />
    </label>
    {#if missing.length}
      <details>
        <summary>{$t('import.missing', { n: missing.length })}</summary>
        <ul>
          {#each missing.slice(0, 100) as tr}<li>{label(tr)}</li>{/each}
        </ul>
      </details>
    {/if}
    <div class="row">
      <div class="spacer"></div>
      <button type="button" class="pill primary lg" on:click={create} disabled={busy || !found.length}>
        {#if busy}<LoaderCircle class="spin" size={16} />{/if}
        {$t('import.create')}
      </button>
    </div>
  {/if}

  {#if error}<p class="error-text">{error}</p>{/if}
</Dialog>

<style>
  p {
    margin: 0;
  }
  .hint {
    color: var(--text-dim);
    font-size: 14px;
  }
  .warn {
    color: var(--warn);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .spacer {
    flex: 1;
  }
  .bar {
    height: 6px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.15);
    overflow: hidden;
  }
  .bar div {
    height: 100%;
    background: var(--accent);
    transition: width 0.3s;
  }
  .summary {
    font-size: 18px;
    font-weight: 700;
  }
  details {
    font-size: 14px;
    color: var(--text-dim);
  }
  summary {
    cursor: pointer;
  }
  ul {
    margin: 8px 0 0;
    padding-left: 18px;
    max-height: 180px;
    overflow-y: auto;
  }
</style>
