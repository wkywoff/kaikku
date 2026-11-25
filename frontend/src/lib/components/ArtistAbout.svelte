<script>
  import { api } from '../api.js';
  import { artistBios as bioCache } from '../artistCache.js';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { toast } from '../stores/toast.js';
  import Pencil from '@lucide/svelte/icons/pencil';
  import X from '@lucide/svelte/icons/x';
  import SectionHead from './SectionHead.svelte';
  import { paths, link } from '../stores/nav.js';
  import { tagSlug } from '../mixes.js';

  export let artistId;

  const MAX_TAGS = 5;

  let bio = null;
  let expanded = false;
  let editing = false;
  let draft = '';
  let busy = false;

  $: load(artistId, $lang);

  let seq = 0;
  async function load(id, l) {
    const my = ++seq;
    const key = `${id}:${l}`;
    bio = bioCache.get(key) || null;
    expanded = editing = editingTags = false;
    try {
      const b = await api.artistBio(id, l);
      bioCache.set(key, b);
      if (my === seq) bio = b;
    } catch {
    }
  }

  $: long = !!bio?.text && (bio.text.length > 420 || bio.text.split('\n').length > 4);
  $: sourceName = bio?.source === 'lastfm' ? 'Last.fm' : bio?.source === 'deezer' ? 'Deezer' : '';

  function startEdit() {
    draft = bio?.text || '';
    editing = true;
  }

  let editingTags = false;
  let tagDraft = [];
  let tagInput = '';

  function startTags() {
    tagDraft = [...(bio?.tags || [])];
    tagInput = '';
    editingTags = true;
  }

  function addTag() {
    const tag = tagInput.trim().toLowerCase();
    if (!tag) return true;
    if (!tagDraft.includes(tag)) {
      if (tagDraft.length >= MAX_TAGS) {
        toast($t('artist.tagsMax', { n: MAX_TAGS }), 'error');
        return false;
      }
      tagDraft = [...tagDraft, tag];
    }
    tagInput = '';
    return true;
  }

  async function saveTags(fn) {
    busy = true;
    try {
      const r = await fn();
      bio = { ...bio, tags: r.tags, tags_custom: r.tags_custom };
      bioCache.delete(`${artistId}:ru`);
      bioCache.delete(`${artistId}:en`);
      bioCache.set(`${artistId}:${$lang}`, bio);
      editingTags = false;
      toast($t('artist.tagsSaved'));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      busy = false;
    }
  }

  async function save(fn) {
    busy = true;
    try {
      const b = await fn();
      bioCache.delete(`${artistId}:ru`);
      bioCache.delete(`${artistId}:en`);
      bioCache.set(`${artistId}:${$lang}`, b);
      bio = b;
      editing = false;
      toast($t('artist.bioSaved'));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      busy = false;
    }
  }
</script>

{#if bio && (bio.text || bio.editable || bio.tags?.length)}
  <section>
    <SectionHead title={$t('artist.about')}>
      <svelte:fragment slot="extra">
        {#if bio.editable && !editing && bio.text}
          <button class="icon-btn" title={$t('artist.bioEdit')} aria-label={$t('artist.bioEdit')} on:click={startEdit}><Pencil size={18} /></button>
        {/if}
      </svelte:fragment>
    </SectionHead>

    {#if editing}
      <form on:submit|preventDefault={() => save(() => api.setArtistBio(artistId, draft))}>
        <textarea class="input" bind:value={draft} rows="8" maxlength="20000" placeholder={$t('artist.bioPlaceholder')} aria-label={$t('artist.about')}></textarea>
        <p class="hint">{$t('artist.bioHint')}</p>
        <div class="buttons">
          {#if bio.source === 'custom'}
            <button type="button" class="pill outline reset" disabled={busy} on:click={() => save(() => api.resetArtistBio(artistId))}>{$t('artist.bioReset')}</button>
          {/if}
          <button type="button" class="pill outline" disabled={busy} on:click={() => (editing = false)}>{$t('settings.cancel')}</button>
          <button type="submit" class="pill primary" disabled={busy}>{$t('edit.save')}</button>
        </div>
      </form>
    {:else}
      {#if bio.text || bio.tags?.length || editingTags}
        <div class="card">
          {#if bio.text}
            <p class="text" class:clamped={long && !expanded}>{bio.text}</p>
            {#if long}
              <button class="text-btn more" on:click={() => (expanded = !expanded)}>{expanded ? $t('artist.bioLess') : $t('artist.bioMore')}</button>
            {/if}
          {/if}
          {#if editingTags}
            <form class="tag-edit" class:alone={!bio.text} on:submit|preventDefault={addTag}>
              <ul class="tags editing" aria-label={$t('artist.tags')}>
                {#each tagDraft as tag, i (tag)}
                  <li>
                    <span>{tag}</span>
                    <button type="button" class="remove" aria-label={$t('artist.tagRemove', { tag })} title={$t('artist.tagRemove', { tag })} on:click={() => (tagDraft = tagDraft.filter((_, j) => j !== i))}><X size={14} /></button>
                  </li>
                {/each}
              </ul>
              <!-- svelte-ignore a11y-autofocus -->
              <input class="input" bind:value={tagInput} maxlength="40" autofocus placeholder={$t('artist.tagPlaceholder')} aria-label={$t('artist.tagPlaceholder')} />
              <div class="buttons">
                {#if bio.tags_custom}
                  <button type="button" class="pill outline reset" disabled={busy} on:click={() => saveTags(() => api.resetArtistTags(artistId))}>{$t('artist.tagsReset')}</button>
                {/if}
                <button type="button" class="pill outline" disabled={busy} on:click={() => (editingTags = false)}>{$t('settings.cancel')}</button>
                <button type="button" class="pill primary" disabled={busy} on:click={() => addTag() && saveTags(() => api.setArtistTags(artistId, tagDraft))}>{$t('edit.save')}</button>
              </div>
            </form>
          {:else if bio.tags?.length}
            <ul class="tags" class:alone={!bio.text} aria-label={$t('artist.tags')}>
              {#each bio.tags as tag}<li><a class="chip" href={paths.genre(tagSlug(tag))} use:link>{tag}</a></li>{/each}
              {#if bio.editable}
                <li><button class="edit-tags" title={$t('artist.tagsEdit')} aria-label={$t('artist.tagsEdit')} on:click={startTags}><Pencil size={13} /></button></li>
              {/if}
            </ul>
          {/if}
          {#if bio.text && sourceName}
            <a class="source" href={bio.url} target="_blank" rel="noopener">{$t('artist.bioFrom', { source: sourceName })}</a>
          {/if}
        </div>
      {/if}
      {#if bio.editable && ((!bio.text && !editing) || (!bio.tags?.length && !editingTags))}
        <div class="adds" class:spaced={bio.text || bio.tags?.length || editingTags}>
          {#if !bio.text}<button class="pill outline" on:click={startEdit}>{$t('artist.bioAdd')}</button>{/if}
          {#if !bio.tags?.length && !editingTags}<button class="pill outline" on:click={startTags}>{$t('artist.tagsAdd')}</button>{/if}
        </div>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .card {
    max-width: 760px;
    padding: 20px 24px;
    border-radius: var(--radius);
    background: var(--bg-card);
  }
  .text {
    margin: 0;
    white-space: pre-line;
    line-height: 1.55;
    font-size: 15px;
    color: var(--text-dim);
    overflow-wrap: anywhere;
  }
  .text.clamped {
    display: -webkit-box;
    -webkit-line-clamp: 5;
    line-clamp: 5;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .more {
    margin-top: 8px;
  }
  @media (hover: hover) {
    .source:hover {
      text-decoration: underline;
    }
    .tags .chip:hover {
      background: var(--bg-hover);
      color: var(--text);
    }
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin: 14px 0 0;
    padding: 0;
    list-style: none;
  }
  .tags.alone {
    margin-top: 0;
  }
  .adds {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .adds.spaced {
    margin-top: 12px;
  }
  .edit-tags {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 24px;
    border-radius: 999px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .edit-tags:hover {
      background: var(--bg-hover);
      color: var(--text);
    }
  }
  .tag-edit {
    margin-top: 14px;
    gap: 10px;
  }
  .tag-edit.alone {
    margin-top: 0;
  }
  .tags.editing {
    margin: 0;
  }
  .tags.editing:empty {
    display: none;
  }
  .tags.editing li {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 3px 4px 3px 10px;
    border-radius: 999px;
    background: var(--bg-hover);
    color: var(--text);
    font-size: 13px;
  }
  .remove {
    display: flex;
    padding: 2px;
    border-radius: 50%;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .remove:hover {
      background: var(--bg-press);
      color: var(--text);
    }
  }
  .tag-edit .input {
    max-width: 320px;
  }
  .source {
    display: block;
    margin-top: 12px;
    font-size: 12px;
    color: var(--text-dim);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 760px;
  }
  .hint {
    margin: 0;
    font-size: 12px;
    color: var(--text-dim);
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .reset {
    margin-right: auto;
  }
  @media (max-width: 768px) {
    .card {
      padding: 16px;
    }
  }
</style>
