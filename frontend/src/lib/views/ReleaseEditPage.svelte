<script>
  import { navStore, paths, link } from '../stores/nav.js';
  import { canEdit } from '../stores/me.js';
  import { t } from '../i18n.js';
  import { isLocalId } from '../links.js';
  import OwnReleaseEditor from '../components/release/OwnReleaseEditor.svelte';
  import DeezerReleaseEditor from '../components/release/DeezerReleaseEditor.svelte';

  const id = $navStore.id;
</script>

{#if isLocalId(id)}
  <OwnReleaseEditor {id} />
{:else if $canEdit}
  <DeezerReleaseEditor {id} />
{:else}
  <div class="page-pad denied">
    <p>{$t('edit.adminOnly')}</p>
    <a class="pill outline" href={paths.album(id)} use:link>{$t('edit.backToRelease')}</a>
  </div>
{/if}

<style>
  .denied {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
    padding-top: 48px;
    color: var(--text-dim);
  }
</style>
