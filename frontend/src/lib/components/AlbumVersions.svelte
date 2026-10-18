<script>
  // «Другие версии» под треклистом: делюкс, clean и другие релизы, которые
  // админ привязал к основному (internal/api/versions.go). На странице
  // версии здесь и основной релиз. Все карточки, без «Показать все».
  import { paths } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import MediaCard from './MediaCard.svelte';

  export let versions = []; // альбомы Deezer (с правками)
  export let explicit = false; // текущий релиз

  // Clean/Explicit — только если в группе есть и те, и другие.
  $: mixed = versions.some((v) => !!v.explicit !== explicit);
  const subtitle = (v, mixed) => [year(v.release_date), mixed && $t(v.explicit ? 'album.explicit' : 'album.clean')].filter(Boolean).join(' • ');
</script>

{#if versions.length}
  <section>
    <h2>{$t('album.versions')}</h2>
    <div class="grid">
      {#each versions as v (v.id)}
        <MediaCard href={paths.album(v.id)} menu={() => albumMenuItems(v)} image={v.cover_url} hidden={v.hidden} title={v.title} subtitle={subtitle(v, mixed)} />
      {/each}
    </div>
  </section>
{/if}

<style>
  section {
    margin-top: 40px;
  }
  h2 {
    font-size: 24px;
    margin-bottom: 8px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  @media (max-width: 768px) {
    h2 {
      font-size: 20px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
