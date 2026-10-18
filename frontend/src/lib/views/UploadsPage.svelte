<script>
  // «Мои загрузки» (/upload): свои релизы — черновики и опубликованные — и
  // «Новый релиз»; у админа — все загруженные, с именем автора у чужих.
  // Карточка открывает окно загрузки: правят здесь, а не на странице релиза.
  // Новый релиз сразу заводится черновиком на сервере и открывается окном
  // загрузки (/upload/<id>): адрес не меняется посреди правки, а брошенный
  // черновик найдётся здесь.
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { me } from '../stores/me.js';
  import { paths, navigate } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { toast } from '../stores/toast.js';
  import { recordTypeLabel } from '../format.js';
  import MediaCard from '../components/MediaCard.svelte';
  import Plus from '@lucide/svelte/icons/plus';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  pageTint().none();

  let releases = null;
  let error = '';
  let creating = false;

  api
    .uploads()
    .then((d) => (releases = d.releases))
    .catch((e) => (error = e.message));

  async function newRelease() {
    creating = true;
    try {
      const { id } = await api.createRelease();
      navigate(paths.upload(id));
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

<div class="page-pad uploads">
  <div class="top">
    <h1>{$t('upload.title')}</h1>
    {#if $me?.can_upload}
      <button class="pill new" disabled={creating} on:click={newRelease}>
        {#if creating}<LoaderCircle class="spin" size={16} />{:else}<Plus size={16} />{/if}
        {$t('upload.newRelease')}
      </button>
    {/if}
  </div>
  <p class="muted intro">{$t('upload.intro')}</p>

  {#if error}
    <p class="error-text">{error}</p>
  {:else if releases && !releases.length}
    <div class="empty">
      <h2>{$t('upload.empty')}</h2>
      <p class="muted">{$t('upload.emptyText')}</p>
    </div>
  {:else if releases}
    <div class="grid">
      {#each releases as r (r.id)}
        <MediaCard href={paths.upload(r.id)} image={r.cover_url || null} title={r.title || $t('upload.untitled')} subtitle={subtitle(r)} />
      {/each}
    </div>
  {:else}
    <div class="loading"><LoaderCircle class="spin" size={24} /></div>
  {/if}
</div>

<style>
  .uploads {
    padding-top: 24px;
    padding-bottom: 24px;
  }
  .top {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  h1 {
    font-size: 32px;
  }
  .new {
    height: 40px;
    padding: 0 18px;
    background: var(--text);
    color: var(--bg);
    font-weight: 700;
  }
  @media (hover: hover) {
    .new:hover:not(:disabled) {
      background: var(--text);
    }
  }
  .intro {
    margin: 8px 0 24px;
    max-width: 640px;
    font-size: 14px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .empty {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 32px 0;
    max-width: 480px;
  }
  .loading {
    padding: 48px 0;
    color: var(--text-dim);
  }
  @media (max-width: 768px) {
    h1 {
      font-size: 26px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
