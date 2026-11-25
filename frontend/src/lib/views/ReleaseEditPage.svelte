<script>
  import { navStore, paths, link } from '../stores/nav.js';
  import { canEdit } from '../stores/me.js';
  import { t } from '../i18n.js';
  import { isLocalId } from '../links.js';
  import { pageTint } from '../stores/theme.js';
  import EmptyState from '../components/EmptyState.svelte';
  import OwnReleaseEditor from '../components/release/OwnReleaseEditor.svelte';
  import DeezerReleaseEditor from '../components/release/DeezerReleaseEditor.svelte';

  const id = $navStore.id;
  const denied = !isLocalId(id) && !$canEdit;
  if (denied) pageTint().none();
</script>

{#if isLocalId(id)}
  <OwnReleaseEditor {id} />
{:else if $canEdit}
  <DeezerReleaseEditor {id} />
{:else}
  <div class="page-pad">
    <EmptyState title={$t('edit.adminOnly')}>
      <a class="pill outline" href={paths.album(id)} use:link>{$t('edit.backToRelease')}</a>
    </EmptyState>
  </div>
{/if}
