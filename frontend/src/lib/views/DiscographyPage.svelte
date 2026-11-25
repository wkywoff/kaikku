<script context="module">
  const PREFS_KEY = 'kaikku_discography';

  function loadPrefs() {
    try {
      const p = JSON.parse(localStorage.getItem(PREFS_KEY) || '{}');
      return p.sort === 'name' ? { sort: 'name', desc: false } : { sort: 'date', desc: p.desc !== false };
    } catch {
      return { sort: 'date', desc: true };
    }
  }
</script>

<script>
  import { api } from '../api.js';
  import { artistPages as cache } from '../artistCache.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, navigate, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { setRouteTitle } from '../stores/title.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { playingContext, isContext, playAlbum } from '../library.js';
  import { recordCategory, recordTypeLabel, year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import SortButton from '../components/SortButton.svelte';
  import StickyBar from '../components/StickyBar.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';

  let artist = null;
  let albums = [];
  let error = '';
  let prefs = loadPrefs();

  $: id = $navStore.id;
  $: filter = $navStore.filter || 'all';
  $: load(id);

  async function load(artistId) {
    error = '';
    const cached = cache.get(artistId);
    if (cached) ({ artist, albums } = cached);
    try {
      const data = await api.discoverArtist(artistId);
      if (artistId !== $navStore.id) return;
      cache.set(artistId, data);
      artist = data.artist;
      albums = data.albums;
    } catch (e) {
      if (!cached) error = e.message;
    }
  }

  pageTint().none();
  $: if (artist) setRouteTitle(`discography:${id}`, $t('title.discography', { name: artist.name }));

  function savePrefs(next) {
    prefs = next;
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
    } catch {
    }
  }

  const matches = (a, f) => f === 'all' || recordCategory(a.record_type) === f;
  $: shown = albums
    .filter((a) => matches(a, filter))
    .sort((x, y) => {
      const c = prefs.sort === 'name' ? x.title.localeCompare(y.title, $lang) : (x.release_date || '').localeCompare(y.release_date || '');
      return prefs.desc ? -c : c;
    });

  const FILTER_LABELS = { all: 'disc.all', album: 'disc.album', ep: 'disc.ep', single: 'disc.single' };

  function filterMenu(e) {
    openMenu(
      e,
      ['all', 'album', 'ep', 'single']
        .filter((f) => f === 'all' || albums.some((a) => matches(a, f)))
        .map((f) => ({
          label: $t(FILTER_LABELS[f]),
          checked: f === filter,
          onClick: () => navigate(paths.discography(id, f), { replace: true }),
        })),
    );
  }

  $: sortOptions = [
    { key: 'date', label: $t('disc.byDate'), desc: true },
    { key: 'name', label: $t('disc.byName') },
  ];

  $: playingCtx = $playerPlaying ? $playingContext : null;

  function onPlay(albumId) {
    playAlbum(albumId).catch((e) => (error = e.message));
  }

  const subtitle = (a) => [year(a.release_date), recordTypeLabel(a.record_type, $lang)].filter(Boolean).join(' • ');
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if artist}
  <StickyBar>
    <a href={paths.artist(artist.id)} use:link>{artist.name}</a>
    <svelte:fragment slot="aside">
      <button class="drop-btn" on:click={filterMenu} aria-haspopup="menu">{$t(FILTER_LABELS[filter])}<ChevronDown size={16} /></button>
      <SortButton options={sortOptions} value={{ key: prefs.sort, desc: prefs.desc }} on:change={(e) => savePrefs({ sort: e.detail.key, desc: e.detail.desc })} />
    </svelte:fragment>
  </StickyBar>

  <div class="page-pad">
    <CardGrid>
      {#each shown as a (a.id)}
        <MediaCard
          href={paths.album(a.id)}
          menu={() => albumMenuItems(a)}
          image={a.cover_url}
          hidden={a.hidden}
          title={a.title}
          subtitle={subtitle(a)}
          onPlay={() => onPlay(a.id)}
          playing={isContext(playingCtx, 'album', a.id)}
        />
      {/each}
    </CardGrid>
  </div>
{:else}
  <div class="page-pad"><Skeleton kind="bar" /></div>
{/if}
