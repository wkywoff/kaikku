<script>
  // Раздел «Версия релиза» на странице правки (EditShell): привязать релиз
  // к основному по ссылке (делюкс, clean) — он уйдёт из дискографии в
  // «Другие версии» основного — или отвязать. Сохраняется вместе со
  // страницей; versionOf — { id, title, cover_url } или null.
  import { api } from '../../api.js';
  import { t } from '../../i18n.js';
  import { sized } from '../../imageproxy.js';
  import { albumIdFrom, isShortDeezerLink } from '../../links.js';
  import X from '@lucide/svelte/icons/x';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let versionOf = null;
  export let selfId; // сам релиз: к себе не привязать
  export let hint = '';

  let link = '';
  let error = '';
  let busy = false;

  async function add() {
    error = '';
    if (isShortDeezerLink(link)) return (error = $t('edit.shortLink'));
    const dz = albumIdFrom(link);
    if (!dz) return (error = $t('edit.badAlbumLink'));
    if (dz === String(selfId)) return (error = $t('upload.versionSelf'));
    busy = true;
    try {
      const { album } = await api.discoverAlbum(dz);
      versionOf = album;
      link = '';
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }
</script>

<section class="card">
  <h2>{$t('edit.versionOf')} <span class="optional">{$t('upload.optional')}</span></h2>
  <div class="field">
    {#if versionOf}
      <div class="linked">
        {#if versionOf.cover_url}<img src={sized(versionOf.cover_url, 64)} alt="" />{/if}
        <span class="linked-title">{versionOf.title}</span>
        <button type="button" class="icon-btn" title={$t('edit.unlinkVersion')} aria-label={$t('edit.unlinkVersion')} on:click={() => (versionOf = null)}><X size={18} /></button>
      </div>
    {:else}
      <div class="link-row">
        <input type="text" bind:value={link} placeholder={$t('edit.albumLink')} aria-label={$t('edit.versionOf')}
          on:keydown={(e) => e.key === 'Enter' && (e.preventDefault(), add())} />
        <button type="button" class="pill outline" disabled={!link.trim() || busy} on:click={add}>
          {#if busy}<LoaderCircle class="spin" size={16} />{/if}{$t('edit.linkVersion')}
        </button>
      </div>
    {/if}
    {#if error}<p class="error-text">{error}</p>{/if}
    {#if hint}<small>{hint}</small>{/if}
  </div>
</section>

<style>
  .linked {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 56px;
    padding: 6px 6px 6px 8px;
    border-radius: 8px;
    background: #2e2e2e;
  }
  .linked img {
    width: 44px;
    height: 44px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }
  .linked-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
  }
</style>
