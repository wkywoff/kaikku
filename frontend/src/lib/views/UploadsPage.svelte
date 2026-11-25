<script>
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { me } from '../stores/me.js';
  import { paths, navigate } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { toast } from '../stores/toast.js';
  import { recordTypeLabel } from '../format.js';
  import MediaCard from '../components/MediaCard.svelte';
  import PageHead from '../components/PageHead.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import GhostCard from '../components/GhostCard.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Plus from '@lucide/svelte/icons/plus';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  pageTint().none();

  let releases = null;
  let error = '';
  let creating = false;
  let perRow = 2;

  api
    .uploads()
    .then((d) => (releases = d.releases))
    .catch((e) => (error = e.message));

  async function newRelease() {
    creating = true;
    try {
      const { id } = await api.createRelease();
      navigate(paths.releaseEdit(id));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      creating = false;
    }
  }

  const subtitle = (r) =>
    [r.draft ? $t('upload.draft') : null, recordTypeLabel(r.record_type, $lang), !r.draft && r.artist_name, r.owner && $t('upload.by', { name: r.owner })]
      .filter(Boolean)
      .join(' • ');
</script>

<div class="page-pad">
  <PageHead title={$t('upload.title')} intro={$t('upload.intro')}>
    {#if $me?.can_upload}
      <button class="pill primary lg" disabled={creating} on:click={newRelease}>
        {#if creating}<LoaderCircle class="spin" size={16} />{:else}<Plus size={16} />{/if}
        {$t('upload.newRelease')}
      </button>
    {/if}
  </PageHead>

  {#if error}
    <p class="error-text">{error}</p>
  {:else if releases && !releases.length}
    <EmptyState title={$t('upload.empty')} text={$t('upload.emptyText')} />
  {:else}
    <CardGrid bind:perRow>
      {#if releases}
        {#each releases as r (r.id)}
          <MediaCard href={paths.releaseEdit(r.id)} image={r.cover_url || null} title={r.title || $t('upload.untitled')} subtitle={subtitle(r)} />
        {/each}
      {:else}
        {#each Array(perRow) as _}<GhostCard />{/each}
      {/if}
    </CardGrid>
  {/if}
</div>
