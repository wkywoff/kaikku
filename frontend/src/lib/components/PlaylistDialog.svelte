<script>
  // Данные плейлиста (владелец): обложка, название, описание, публичный
  // или нет, соавторы (из друзей), удаление. Публичный видят и слушают все пользователи
  // сервиса. Без своей обложки — мозаика из обложек треков. Соавторы
  // меняются сразу, без «Сохранить»: они добавляют, убирают и
  // переставляют треки, название и обложку меняет только владелец.
  import { modal } from '../modal.js';
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { toSquareJpeg } from '../image.js';
  import Artwork from './Artwork.svelte';
  import X from '@lucide/svelte/icons/x';
  import ImagePlus from '@lucide/svelte/icons/image-plus';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let playlist; // { id, title, description, is_public, covers, custom_cover, collaborators }

  const dispatch = createEventDispatcher();
  let title = playlist.title;
  let description = playlist.description || '';
  let isPublic = playlist.is_public;
  let busy = false;
  let error = '';

  // Соавторы — из друзей (подписаны друг на друга, UserPage); уже позванные
  // остаются в списке, даже если дружба кончилась, — чтобы их можно было убрать.
  let users = [];
  let loadedPeople = false;
  let collab = new Set((playlist.collaborators || []).map((u) => u.id));
  let collabBusy = '';
  api
    .people()
    .then(({ people }) => {
      const friends = people.filter((p) => p.i_follow && p.follows_me);
      const rest = (playlist.collaborators || []).filter((c) => !friends.some((f) => f.id === c.id));
      users = [...friends, ...rest];
    })
    .catch(() => {})
    .finally(() => (loadedPeople = true));
  async function toggleCollaborator(u) {
    collabBusy = u.id;
    error = '';
    try {
      if (collab.has(u.id)) {
        await api.removeCollaborator(playlist.id, u.id);
        collab.delete(u.id);
      } else {
        await api.addCollaborator(playlist.id, u.id);
        collab.add(u.id);
      }
      collab = collab;
      dispatch('collaborators');
    } catch (e) {
      error = e.message;
    } finally {
      collabBusy = '';
    }
  }

  // Обложка: newImage — выбранная (Blob), removeCover — убрать свою.
  let newImage = null;
  let newImageUrl = null;
  let removeCover = false;
  let fileInput;
  $: hasCustom = !!newImage || (playlist.custom_cover && !removeCover);
  $: previewImages = newImageUrl ? [newImageUrl] : playlist.custom_cover && !removeCover ? playlist.covers : [];

  async function pickImage(e) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = '';
    if (!file) return;
    error = '';
    try {
      newImage = await toSquareJpeg(file);
      if (newImageUrl) URL.revokeObjectURL(newImageUrl);
      newImageUrl = URL.createObjectURL(newImage);
      removeCover = false;
    } catch {
      error = $t('edit.badImage');
    }
  }

  function dropImage() {
    if (newImageUrl) URL.revokeObjectURL(newImageUrl);
    newImage = null;
    newImageUrl = null;
    removeCover = !!playlist.custom_cover;
  }
  onDestroy(() => newImageUrl && URL.revokeObjectURL(newImageUrl));

  onMount(() => {
    const onKey = (e) => e.key === 'Escape' && !busy && dispatch('close');
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  async function save() {
    if (!title.trim()) return;
    busy = true;
    error = '';
    try {
      let saved = await api.updatePlaylist(playlist.id, { title, description, is_public: isPublic });
      if (newImage) saved = await api.uploadPlaylistCover(playlist.id, newImage);
      else if (removeCover) saved = await api.deletePlaylistCover(playlist.id);
      dispatch('saved', saved);
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  async function remove() {
    if (!confirm($t('playlist.deleteConfirm', { name: playlist.title }))) return;
    busy = true;
    try {
      await api.deletePlaylist(playlist.id);
      dispatch('deleted');
    } catch (e) {
      error = e.message;
      busy = false;
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={() => !busy && dispatch('close')} transition:fade={{ duration: 120 }} on:click|self={() => !busy && dispatch('close')} role="dialog" tabindex="-1" aria-modal="true" aria-label={$t('playlist.edit')}>
  <form class="dialog" transition:scale={{ start: 0.96, duration: 150 }} on:submit|preventDefault={save}>
    <div class="head">
      <h2>{$t('playlist.edit')}</h2>
      <button type="button" class="icon-btn" on:click={() => dispatch('close')} aria-label={$t('album.close')} disabled={busy}><X size={20} /></button>
    </div>

    <div class="details">
      <div class="cover-col">
        <button type="button" class="cover" on:click={() => fileInput.click()} disabled={busy} aria-label={$t('playlist.chooseCover')} title={$t('playlist.chooseCover')}>
          {#if previewImages.length}
            <img src={previewImages[0]} alt="" />
          {:else}
            <Artwork images={removeCover ? playlist.mosaic || [] : playlist.covers} iconSize={48} />
          {/if}
          <span class="cover-hover"><ImagePlus size={32} /><span>{$t('playlist.chooseCover')}</span></span>
        </button>
        {#if hasCustom}
          <button type="button" class="link" on:click={dropImage} disabled={busy}>{$t('playlist.removeCover')}</button>
        {/if}
        <input bind:this={fileInput} type="file" accept="image/*" hidden on:change={pickImage} />
      </div>
      <div class="fields">
        <label>
          <span>{$t('playlist.name')}</span>
          <!-- svelte-ignore a11y_autofocus -->
          <input type="text" bind:value={title} maxlength="300" required autofocus />
        </label>
        <label>
          <span>{$t('playlist.description')}</span>
          <textarea bind:value={description} maxlength="1000" rows="4" placeholder={$t('playlist.descriptionPlaceholder')}></textarea>
        </label>
      </div>
    </div>
    <div class="row">
      <div class="label">
        <span>{$t('playlist.public')}</span>
        <small>{$t('playlist.publicHint')}</small>
      </div>
      <button type="button" class="switch" role="switch" aria-checked={isPublic} aria-label={$t('playlist.public')} on:click={() => (isPublic = !isPublic)}>
        <span class="knob"></span>
      </button>
    </div>

    {#if loadedPeople}
      <div class="collab">
        <div class="label">
          <span>{$t('playlist.collaborators')}</span>
          <small>{$t(users.length ? 'playlist.collaboratorsHint' : 'playlist.noFriends')}</small>
        </div>
        {#each users as u (u.id)}
          <div class="row user">
            <span class="name">{u.username}</span>
            <button
              type="button"
              class="switch"
              role="switch"
              aria-checked={collab.has(u.id)}
              aria-label={$t('playlist.collaborator', { name: u.username })}
              disabled={!!collabBusy}
              on:click={() => toggleCollaborator(u)}
            >
              <span class="knob"></span>
            </button>
          </div>
        {/each}
      </div>
    {/if}

    {#if error}<p class="error-text">{error}</p>{/if}

    <div class="footer">
      <button type="button" class="delete" on:click={remove} disabled={busy}><Trash2 size={16} /> {$t('playlist.delete')}</button>
      <div class="spacer"></div>
      <button type="submit" class="save" disabled={busy || !title.trim()}>
        {#if busy}<LoaderCircle class="spin" size={16} />{/if}
        {$t('edit.save')}
      </button>
    </div>
  </form>
</div>

<style>
  .collab {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .collab .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 4px;
  }
  .collab .label small {
    color: var(--text-dim);
  }
  .user .name {
    font-weight: 500;
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px 16px max(16px, env(safe-area-inset-bottom));
    background: rgba(0, 0, 0, 0.7);
  }
  .dialog {
    width: min(560px, 100%);
    max-height: 100%; /* по видимой высоте окна (в отступах подложки), а не 100vh — во вкладке браузера адресная строка её отъедает */
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 24px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 6px;
  }
  h2 {
    font-size: 22px;
  }
  .details {
    display: flex;
    gap: 16px;
  }
  .cover-col {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .cover {
    position: relative;
    width: 180px;
    height: 180px;
    flex-shrink: 0;
    border-radius: 4px;
    overflow: hidden;
    background: #3e3e3e;
    box-shadow: 0 4px 60px rgba(0, 0, 0, 0.5);
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .cover-hover {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: 14px;
    font-weight: 600;
    background: rgba(0, 0, 0, 0.6);
    opacity: 0;
    transition: opacity 0.15s;
  }
  .cover:focus-visible .cover-hover {
    opacity: 1;
  }
  @media (hover: hover) {
    .cover:hover .cover-hover {
      opacity: 1;
    }
  }
  .link {
    font-size: 13px;
    color: var(--text-dim);
    text-decoration: underline;
  }
  @media (hover: hover) {
    .link:hover {
      color: var(--text);
    }
  }
  .fields {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  @media (max-width: 560px) {
    .details {
      flex-direction: column;
      align-items: center;
    }
    .fields {
      width: 100%;
    }
  }
  @media (hover: none) {
    .cover-hover {
      opacity: 1;
      background: rgba(0, 0, 0, 0.35);
    }
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
  }
  input,
  textarea {
    padding: 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
    resize: vertical;
  }
  input:focus,
  textarea:focus {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .label {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 15px;
  }
  .label small {
    font-size: 13px;
    color: var(--text-dim);
  }
  .switch {
    position: relative;
    width: 40px;
    height: 24px;
    flex-shrink: 0;
    border-radius: 999px;
    background: #535353;
    transition: background 0.15s;
  }
  .switch[aria-checked='true'] {
    background: var(--ok);
  }
  .knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s;
  }
  .switch[aria-checked='true'] .knob {
    transform: translateX(16px);
  }
  .footer {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 10px;
  }
  .spacer {
    flex: 1;
  }
  .delete {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .delete:hover:not(:disabled) {
      color: var(--danger);
    }
  }
  .save {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 44px;
    padding: 0 28px;
    border-radius: 999px;
    background: #fff;
    color: #000;
    font-weight: 700;
  }
  .save:disabled {
    opacity: 0.6;
  }
</style>
