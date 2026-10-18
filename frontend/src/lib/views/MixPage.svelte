<script>
  // Микс (lib/mixes.js, internal/api/mixes.go): подборка, которая собирается
  // сама из истории и лайков. Его можно слушать и сохранить себе плейлистом
  // — тогда он перестанет меняться.
  import { api } from '../api.js';
  import { player, isPlaying as playerPlaying, shuffleOn } from '../stores/player.js';
  import { navStore, navigate, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { setRouteTitle, APP_NAME } from '../stores/title.js';
  import { refresh, playRows, playingContext, isContext, mixRow } from '../library.js';
  import { createPlaylist } from '../stores/collection.js';
  import { tracksCount, longDuration } from '../format.js';
  import { mixTitle, mixDescription, mixHue, mixColor } from '../mixes.js';
  import Hero from '../components/Hero.svelte';
  import Artwork from '../components/Artwork.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import ListPlus from '@lucide/svelte/icons/list-plus';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import House from '@lucide/svelte/icons/house';

  const id = $navStore.id;
  let mix = null;
  let rows = [];
  let loading = true;
  let missing = false; // у человека сейчас нет такого микса
  let error = '';
  let saving = false;

  pageTint().color(mixColor(id));

  async function load() {
    try {
      const data = await api.mix(id);
      mix = data.mix;
      rows = data.tracks.map(mixRow);
      refresh(rows.map((r) => r.deezerId));
    } catch (e) {
      if (e.status === 404) missing = true;
      else error = e.message;
    } finally {
      loading = false;
    }
  }
  load();

  $: title = mix ? mixTitle(mix, $t) : '';
  $: if (mix) setRouteTitle(`mix:${id}`, title);
  $: context = { kind: 'mix', id, title };
  $: playingHere = isContext($playingContext, 'mix', id);
  $: totalSec = rows.reduce((s, r) => s + (r.durationMs || 0) / 1000, 0);

  function onPlay() {
    if (playingHere) return player.toggle();
    if (rows.length) playRows(rows, 0, context, { chosen: false });
  }

  // Сохранить как плейлист: снимок микса на сегодня, дальше он не меняется
  // (поэтому в описании плейлиста нет «обновляется каждый день»). Сервер
  // берёт данные каждого трека у Deezer — это несколько секунд.
  async function save() {
    saving = true;
    const ids = rows.map((r) => r.deezerId).filter(Boolean);
    const p = await createPlaylist(ids, { title, description: mixDescription(mix, $t) }, { download: false });
    saving = false;
    if (p) navigate(paths.playlist(p.id));
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if missing}
  <div class="page-pad empty">
    <h2>{$t('mix.missing')}</h2>
    <p class="muted">{$t('mix.missingHint')}</p>
    <a class="pill" href={paths.home()} use:link><House size={16} /> {$t('nav.home')}</a>
  </div>
{:else if mix}
  <div class="page-pad">
    <Hero kind={$t('mix.kind')} {title}>
      <Artwork slot="art" kind="mix" images={mix.covers} label={title} hue={mixHue(id)} sizes="(max-width: 768px) min(62vw, 240px), 232px" />
      <svelte:fragment slot="meta">
        <div class="description">{mixDescription(mix, $t)} {$t('mix.daily')}</div>
        <span class="owner">{APP_NAME}</span>
        {#if rows.length}<span class="dot">•</span><span class="muted-meta">{tracksCount(rows.length, $lang)}, {longDuration(totalSec, $lang)}</span>{/if}
      </svelte:fragment>
    </Hero>
  </div>

  {#if rows.length}
    <div class="actions page-pad">
      <PlayButton size={56} playing={playingHere && $playerPlaying} label={playingHere && $playerPlaying ? $t('player.pause') : $t('player.play')} on:click={onPlay} />
      <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
        <Shuffle size={28} />
      </button>
      <button class="icon-btn" title={saving ? $t('mix.saving') : $t('mix.save')} aria-label={saving ? $t('mix.saving') : $t('mix.save')} aria-busy={saving} disabled={saving} on:click={save}>
        {#if saving}<LoaderCircle size={24} class="spin" />{:else}<ListPlus size={24} />{/if}
      </button>
    </div>
    <div class="page-pad">
      <TrackList {rows} showCover showAlbum numbered {context} />
    </div>
  {/if}
{:else if loading}
  <div class="page-pad skeleton">
    <div class="sk-art"></div>
    <div class="sk-lines"><div></div><div></div></div>
  </div>
{/if}

<style>
  .description {
    width: 100%;
    margin-bottom: 6px;
    color: rgba(255, 255, 255, 0.7);
    font-weight: 400;
  }
  .owner {
    font-weight: 700;
  }
  .dot {
    margin: 0 4px;
  }
  .muted-meta {
    color: rgba(255, 255, 255, 0.7);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 24px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  .icon-btn.big {
    width: 44px;
    height: 44px;
  }
  .empty {
    padding-top: 48px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  .empty h2 {
    font-size: 22px;
  }
  .skeleton {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    padding-top: 48px;
  }
  .sk-art {
    width: 232px;
    height: 232px;
    border-radius: 6px;
    background: var(--bg-card);
  }
  .sk-lines div {
    height: 48px;
    width: 360px;
    max-width: 50vw;
    border-radius: 8px;
    background: var(--bg-card);
    margin-top: 12px;
  }
  @media (max-width: 768px) {
    .skeleton {
      flex-direction: column;
      align-items: center;
    }
    .sk-art {
      width: min(62vw, 240px);
      height: min(62vw, 240px);
    }
  }
</style>
