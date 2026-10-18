<script context="module">
  const PREFS_KEY = 'kaikku_discography';

  function loadPrefs() {
    try {
      const p = JSON.parse(localStorage.getItem(PREFS_KEY) || '{}');
      return { sort: p.sort === 'name' ? 'name' : 'date', desc: p.desc !== false };
    } catch {
      return { sort: 'date', desc: true };
    }
  }
</script>

<script>
  // Вся дискография артиста, как у Spotify (/artist/<id>/discography/all):
  // фильтр «Все / Альбомы / Мини-альбомы / Синглы» (он же в адресе), сортировка по
  // дате или названию (запоминается на этом устройстве), сетка обложек.
  // Треклисты не грузим заранее — только по Play (library.js → playAlbum),
  // чтобы не дёргать Deezer десятками запросов на одну страницу.
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
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';

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
      /* не запомнится — не страшно */
    }
  }

  const matches = (a, f) => f === 'all' || recordCategory(a.record_type) === f;
  $: shown = albums
    .filter((a) => matches(a, filter))
    .sort((x, y) => {
      const c = prefs.sort === 'name' ? x.title.localeCompare(y.title, $lang) : (x.release_date || '').localeCompare(y.release_date || '');
      return prefs.sort === 'name' ? c : prefs.desc ? -c : c;
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

  function sortMenu(e) {
    const arrow = prefs.desc ? ArrowDown : ArrowUp;
    openMenu(e, [
      { heading: $t('disc.sortBy') },
      {
        label: $t('disc.byDate'),
        checked: prefs.sort === 'date',
        icon: prefs.sort === 'date' ? arrow : null,
        // Повторный выбор меняет направление: новые первыми ↔ старые первыми.
        onClick: () => savePrefs({ ...prefs, sort: 'date', desc: prefs.sort === 'date' ? !prefs.desc : true }),
      },
      { label: $t('disc.byName'), checked: prefs.sort === 'name', onClick: () => savePrefs({ ...prefs, sort: 'name' }) },
    ]);
  }

  // Какой альбом сейчас играет (если включён отсюда или с его страницы).
  $: playingCtx = $playerPlaying ? $playingContext : null;

  function onPlay(albumId) {
    playAlbum(albumId).catch((e) => (error = e.message));
  }

  const subtitle = (a) => [year(a.release_date), recordTypeLabel(a.record_type, $lang)].filter(Boolean).join(' • ');
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if artist}
  <div class="bar page-pad">
    <a class="name" href={paths.artist(artist.id)} use:link>{artist.name}</a>
    <div class="controls">
      <button class="ctl" on:click={filterMenu} aria-haspopup="menu">
        {$t(FILTER_LABELS[filter])}
        <ChevronDown size={16} />
      </button>
      <button class="ctl" on:click={sortMenu} aria-haspopup="menu" aria-label={$t('disc.sortBy')}>
        {prefs.sort === 'name' ? $t('disc.byName') : $t('disc.byDate')}
        <ChevronDown size={16} />
      </button>
    </div>
  </div>

  <div class="page-pad">
    <div class="grid">
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
    </div>
  </div>
{:else}
  <div class="page-pad">
    <div class="sk-bar"></div>
  </div>
{/if}

<style>
  .bar {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding-top: 20px;
    padding-bottom: 16px;
    background: var(--bg);
    /* Содержимое — в колонке страницы (App.svelte), а фон полосы — во всю
       ширину: тень без размытия продолжает его в стороны, clip-path
       оставляет её только по высоте полосы. */
    box-shadow: 0 0 0 100vmax var(--bg);
    clip-path: inset(0 -100vmax);
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 24px;
    font-weight: 700;
  }
  @media (hover: hover) {
    .name:hover {
      text-decoration: underline;
    }
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 20px;
    flex-shrink: 0;
  }
  .ctl {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .ctl:hover {
      color: var(--text);
    }
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .sk-bar {
    height: 32px;
    width: 240px;
    margin-top: 24px;
    border-radius: 8px;
    background: var(--bg-card);
  }
  @media (max-width: 768px) {
    .bar {
      padding-top: 12px;
    }
    .name {
      font-size: 20px;
    }
    .controls {
      gap: 14px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
