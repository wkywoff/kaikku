<script>
  import { paths } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { playingContext, isContext, playAlbum } from '../library.js';
  import { toast } from '../stores/toast.js';
  import MediaCard from './MediaCard.svelte';
  import SectionHead from './SectionHead.svelte';
  import CardGrid from './CardGrid.svelte';

  export let versions = [];
  export let explicit = false;

  $: mixed = versions.some((v) => !!v.explicit !== explicit);
  const subtitle = (v, mixed) => [year(v.release_date), mixed && $t(v.explicit ? 'album.explicit' : 'album.clean')].filter(Boolean).join(' • ');

  $: playingCtx = $playerPlaying ? $playingContext : null;
  const play = (id) => playAlbum(id).catch((e) => toast(e.message, 'error'));
</script>

{#if versions.length}
  <section>
    <SectionHead title={$t('album.versions')} />
    <CardGrid>
      {#each versions as v (v.id)}
        <MediaCard
          href={paths.album(v.id)}
          menu={() => albumMenuItems(v)}
          image={v.cover_url}
          hidden={v.hidden}
          title={v.title}
          subtitle={subtitle(v, mixed)}
          onPlay={() => play(v.id)}
          playing={isContext(playingCtx, 'album', v.id)}
        />
      {/each}
    </CardGrid>
  </section>
{/if}
