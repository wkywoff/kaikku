<script>
  // Итоги года, как Spotify Wrapped: минуты, главный исполнитель и трек,
  // топы, месяцы и часы, рекорды. Всё считает сервер (handlers_wrapped.go)
  // в часовом поясе браузера; топ-50 треков можно включить или сохранить
  // плейлистом.
  import { api } from '../api.js';
  import { navStore, navigate, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { sized } from '../imageproxy.js';
  import { refresh, playRows, playAlbum } from '../library.js';
  import { createPlaylist } from '../stores/collection.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import { plural, longDate } from '../format.js';
  import TrackList from '../components/TrackList.svelte';
  import MediaCard from '../components/MediaCard.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import ListPlus from '@lucide/svelte/icons/list-plus';
  import UserRound from '@lucide/svelte/icons/user-round';
  import Disc3 from '@lucide/svelte/icons/disc-3';

  let data = null;
  let error = '';
  let saving = false;

  pageTint().color({ r: 155, g: 31, b: 232 });

  $: load($navStore.year);
  let seq = 0;
  async function load(year) {
    const my = ++seq;
    error = '';
    try {
      const d = await api.wrapped(year);
      if (my === seq) data = d;
    } catch (e) {
      if (my === seq) error = e.message;
    }
  }

  // Номер в списке — место в топе, а не номер трека в альбоме.
  $: rows = (data?.top_tracks || []).map((tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null }));
  $: topArtist = data?.top_artists?.[0];
  $: topTrack = data?.top_tracks?.[0];
  $: maxMonth = Math.max(1, ...(data?.months || [0]));
  $: maxHour = Math.max(1, ...(data?.hours || [0]));
  $: bestMonth = data ? data.months.indexOf(Math.max(...data.months)) : -1;
  // История пишется не с начала года — скажем, откуда итоги.
  $: partial = !!data?.since && data.since > `${data.year}-01-15`;

  const monthName = (m, l) => new Date(2000, m, 1).toLocaleDateString(l === 'en' ? 'en-US' : 'ru-RU', { month: 'long' });
  const monthShort = (m, l) => new Date(2000, m, 1).toLocaleDateString(l === 'en' ? 'en-US' : 'ru-RU', { month: 'narrow' });
  const num = (n, l) => (n || 0).toLocaleString(l === 'en' ? 'en-US' : 'ru-RU');

  // Время суток, когда музыки больше всего.
  function persona(hours) {
    const parts = { night: [0, 6], morning: [6, 12], day: [12, 18], evening: [18, 24] };
    let best = 'day';
    let max = -1;
    for (const [key, [from, to]] of Object.entries(parts)) {
      const sum = hours.slice(from, to).reduce((a, b) => a + b, 0);
      if (sum > max) [best, max] = [key, sum];
    }
    return best;
  }

  // Склонения: «1 прослушивание», «2 раза», «5 минут».
  const FORMS = {
    plays: { ru: ['прослушивание', 'прослушивания', 'прослушиваний'], en: ['play', 'plays'] },
    minutes: { ru: ['минута', 'минуты', 'минут'], en: ['minute', 'minutes'] },
    times: { ru: ['раз', 'раза', 'раз'], en: ['time', 'times'] },
    tracks: { ru: ['разный трек', 'разных трека', 'разных треков'], en: ['different song', 'different songs'] },
    artists: { ru: ['исполнитель', 'исполнителя', 'исполнителей'], en: ['artist', 'artists'] },
    days: { ru: ['день подряд с музыкой', 'дня подряд с музыкой', 'дней подряд с музыкой'], en: ['day in a row with music', 'days in a row with music'] },
  };
  const word = (n, key, l) => (l === 'en' ? FORMS[key].en[n === 1 ? 0 : 1] : plural(n, FORMS[key].ru));
  const count = (n, key, l) => `${num(n, l)} ${word(n, key, l)}`;

  // 12 345 минут → «≈ 8 дней 13 часов»
  function inDays(minutes, l) {
    const d = Math.floor(minutes / 1440);
    const h = Math.floor((minutes % 1440) / 60);
    if (l === 'en') return d ? `${d} day${d === 1 ? '' : 's'} ${h} h` : `${h} hour${h === 1 ? '' : 's'}`;
    const days = `${d} ${plural(d, ['день', 'дня', 'дней'])}`;
    const hours = `${h} ${plural(h, ['час', 'часа', 'часов'])}`;
    return d ? `${days} ${hours}` : hours;
  }

  async function playTop() {
    if (!rows.length) return;
    await refresh(rows.map((r) => r.deezerId));
    playRows(rows, 0, null);
  }

  async function saveTop() {
    saving = true;
    const p = await createPlaylist(
      rows.map((r) => r.deezerId),
      { title: $t('wrapped.playlistTitle', { year: data.year }) },
    );
    saving = false;
    if (p) navigate(paths.playlist(p.id));
  }
</script>

<div class="page-pad wrapped">
  {#if error}
    <p class="error-text">{error}</p>
  {:else if data}
    <div class="head">
      <h1>{$t('wrapped.heading', { year: data.year })}</h1>
      {#if data.years.length > 1}
        <div class="years">
          {#each data.years as y}
            <a class="pill" class:active={y === data.year} href={paths.wrapped(y)} use:link>{y}</a>
          {/each}
        </div>
      {/if}
    </div>

    {#if data.plays === 0}
      <div class="card hero">
        <p class="big-text">{$t('wrapped.empty')}</p>
        <p>{$t('wrapped.emptyHint')}</p>
      </div>
    {:else}
      <section class="card hero">
        <p class="kicker">{$t('wrapped.minutesKicker', { year: data.year })}</p>
        <p class="huge num">{num(data.minutes, $lang)}</p>
        <p class="big-text">{word(data.minutes, 'minutes', $lang)} {$t('wrapped.ofMusicWord')}</p>
        <p>≈ {inDays(data.minutes, $lang)} {$t('wrapped.ofMusic')}</p>
        {#if partial}<p class="note">{$t('wrapped.since', { date: longDate(data.since, $lang) })}</p>{/if}
      </section>

      {#if topArtist}
        <section class="card artist">
          <p class="kicker">{$t('wrapped.topArtist')}</p>
          <a class="artist-hero" href={paths.artist(topArtist.id)} use:link>
            {#if topArtist.picture_url}<img src={sized(topArtist.picture_url, 320)} alt="" />{:else}<span class="ph"><UserRound size={64} /></span>{/if}
            <span class="huge name">{topArtist.name}</span>
          </a>
          <p>{count(topArtist.plays, 'plays', $lang)} · {count(topArtist.minutes, 'minutes', $lang)}</p>
          {#if data.top_artist_listeners?.listeners > 1}
            <p class="badge">
              {data.top_artist_listeners.rank === 1
                ? $t('wrapped.topListener')
                : $t('wrapped.listenerRank', { rank: data.top_artist_listeners.rank, n: data.top_artist_listeners.listeners })}
            </p>
          {/if}
          {#if data.top_artists.length > 1}
            <ol class="ranked">
              {#each data.top_artists as a, i (a.id)}
                <li>
                  <span class="rank num">{i + 1}</span>
                  {#if a.picture_url}<img class="round" src={sized(a.picture_url, 64)} alt="" />{:else}<span class="round ph"><UserRound size={20} /></span>{/if}
                  <a href={paths.artist(a.id)} use:link>{a.name}</a>
                  <span class="muted-small">{count(a.plays, 'plays', $lang)}</span>
                </li>
              {/each}
            </ol>
          {/if}
        </section>
      {/if}

      {#if topTrack}
        <section class="card song">
          <p class="kicker">{$t('wrapped.topSong')}</p>
          <div class="song-hero">
            {#if topTrack.cover_url}<img src={sized(topTrack.cover_url, 300)} alt="" />{:else}<span class="ph square"><Disc3 size={64} /></span>{/if}
            <div>
              <p class="big-text">{topTrack.title}</p>
              <p>{rows[0].artists.map((a) => a.name).join(', ')}</p>
              <p class="badge">{$t('wrapped.songPlays', { times: count(topTrack.plays, 'times', $lang) })}</p>
            </div>
          </div>
        </section>
      {/if}

      <section class="plain">
        <div class="plain-head">
          <h2>{$t('wrapped.topSongs')}</h2>
          <div class="plain-actions">
            <button class="pill" on:click={saveTop} disabled={saving}><ListPlus size={16} /> {$t('wrapped.save')}</button>
            <PlayButton size={48} label={$t('player.play')} on:click={playTop} />
          </div>
        </div>
        <TrackList rows={rows.slice(0, 10)} showCover showAlbum header={false} />
      </section>

      {#if data.top_albums.length}
        <section class="plain">
          <h2>{$t('wrapped.topAlbums')}</h2>
          <div class="grid">
            {#each data.top_albums as a (a.id)}
              <MediaCard href={paths.album(a.id)} image={a.cover_url} title={a.title} subtitle={count(a.plays, 'plays', $lang)} onPlay={() => playAlbum(a.id)} />
            {/each}
          </div>
        </section>
      {/if}

      <section class="card months">
        <p class="kicker">{$t('wrapped.byMonth')}</p>
        {#if bestMonth >= 0}<p class="big-text">{$t('wrapped.bestMonth', { month: monthName(bestMonth, $lang) })}</p>{/if}
        <div class="bars" role="img" aria-label={$t('wrapped.byMonth')}>
          {#each data.months as m, i}
            <div class="bar-col" title="{monthName(i, $lang)}: {m} {$t('wrapped.min')}">
              <div class="bar" class:best={i === bestMonth} style="height: {(m / maxMonth) * 100}%"></div>
              <span>{monthShort(i, $lang)}</span>
            </div>
          {/each}
        </div>
      </section>

      <section class="card hours">
        <p class="kicker">{$t('wrapped.byHour')}</p>
        <p class="big-text">{$t(`wrapped.persona.${persona(data.hours)}`)}</p>
        <div class="bars hours-bars" role="img" aria-label={$t('wrapped.byHour')}>
          {#each data.hours as h, i}
            <div class="bar-col" title="{i}:00 — {h}">
              <div class="bar" style="height: {(h / maxHour) * 100}%"></div>
              <span>{i % 6 === 0 ? i : ''}</span>
            </div>
          {/each}
        </div>
      </section>

      <section class="card facts">
        <p class="kicker">{$t('wrapped.facts')}</p>
        <div class="fact-grid">
          <div><p class="huge num">{num(data.tracks, $lang)}</p><p>{word(data.tracks, 'tracks', $lang)}</p></div>
          <div><p class="huge num">{num(data.artists, $lang)}</p><p>{word(data.artists, 'artists', $lang)}</p></div>
          <div><p class="huge num">{data.streak}</p><p>{word(data.streak, 'days', $lang)}</p></div>
          {#if data.top_day}
            <div>
              <p class="huge num">{num(data.top_day.minutes, $lang)}</p>
              <p>{word(data.top_day.minutes, 'minutes', $lang)} {$t('wrapped.topDay', { date: longDate(data.top_day.date, $lang) })}</p>
            </div>
          {/if}
        </div>
        {#if data.first}
          <p class="first">
            {$t('wrapped.first')}
            <a href={paths.albumTrack(paths.album(data.first.album_id), data.first.id)} use:link>{data.first.title}</a>
            — {data.first.artist}
          </p>
        {/if}
      </section>
    {/if}
  {/if}
</div>

<style>
  .wrapped {
    padding-top: 24px;
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  .wrapped p {
    margin: 0;
  }
  .card .num {
    color: inherit;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  h1 {
    font-size: 40px;
    font-weight: 800;
    letter-spacing: -0.02em;
  }
  h2 {
    font-size: 24px;
    font-weight: 700;
  }
  .years {
    display: flex;
    gap: 8px;
  }
  .card {
    border-radius: 16px;
    padding: 32px;
    color: #fff;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow: hidden;
  }
  .hero {
    background: linear-gradient(135deg, #ff5f6d 0%, #c2185b 45%, #6a11cb 100%);
  }
  .artist {
    background: linear-gradient(160deg, #f77737 0%, #e1306c 100%);
  }
  .song {
    background: linear-gradient(160deg, #1db954 0%, #0b5d33 100%);
  }
  .months {
    background: linear-gradient(160deg, #2d46b9 0%, #1b1f5e 100%);
  }
  .hours {
    background: linear-gradient(160deg, #0f2027 0%, #2c5364 100%);
  }
  .facts {
    background: linear-gradient(160deg, #ff9a00 0%, #ff3d00 100%);
  }
  .kicker {
    font-size: 14px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    opacity: 0.85;
  }
  .huge {
    font-family: var(--font-display);
    font-size: clamp(48px, 9vw, 96px);
    font-weight: 800;
    line-height: 1;
    letter-spacing: -0.03em;
  }
  .big-text {
    font-family: var(--font-display);
    font-size: clamp(22px, 3.5vw, 32px);
    font-weight: 700;
    line-height: 1.15;
  }
  .note {
    font-size: 13px;
    opacity: 0.8;
  }
  .badge {
    align-self: flex-start;
    padding: 6px 12px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.25);
    font-weight: 600;
    font-size: 14px;
  }
  .artist-hero {
    display: flex;
    align-items: center;
    gap: 24px;
    flex-wrap: wrap;
  }
  .artist-hero img,
  .artist-hero .ph {
    width: 160px;
    height: 160px;
    border-radius: 50%;
    object-fit: cover;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
  }
  .ph {
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.25);
  }
  .artist-hero .name {
    font-size: clamp(36px, 7vw, 72px);
    overflow-wrap: anywhere;
  }
  @media (hover: hover) {
    .artist-hero:hover .name {
      text-decoration: underline;
    }
  }
  .ranked {
    list-style: none;
    margin: 12px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .ranked li {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .rank {
    width: 20px;
    font-weight: 700;
    opacity: 0.8;
  }
  .round {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    object-fit: cover;
  }
  .ranked a {
    font-weight: 600;
  }
  @media (hover: hover) {
    .ranked a:hover {
      text-decoration: underline;
    }
  }
  .muted-small {
    margin-left: auto;
    font-size: 13px;
    opacity: 0.8;
  }
  .song-hero {
    display: flex;
    align-items: center;
    gap: 24px;
    flex-wrap: wrap;
  }
  .song-hero img,
  .song-hero .square {
    width: 180px;
    height: 180px;
    border-radius: 8px;
    object-fit: cover;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
  }
  .song-hero div {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .plain {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .plain-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .plain-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .bars {
    display: flex;
    align-items: flex-end;
    gap: 6px;
    height: 180px;
    margin-top: 12px;
  }
  .hours-bars {
    gap: 3px;
  }
  .bar-col {
    flex: 1;
    height: 100%;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    align-items: center;
    gap: 6px;
  }
  .bar {
    width: 100%;
    min-height: 2px;
    border-radius: 4px 4px 0 0;
    background: rgba(255, 255, 255, 0.45);
  }
  .bar.best {
    background: #fff;
  }
  .bar-col span {
    height: 14px;
    font-size: 11px;
    opacity: 0.8;
    text-transform: uppercase;
  }
  .fact-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 20px;
    margin-top: 8px;
  }
  .fact-grid .huge {
    font-size: clamp(40px, 6vw, 64px);
  }
  .first a {
    font-weight: 700;
    text-decoration: underline;
  }
  @media (max-width: 768px) {
    h1 {
      font-size: 28px;
    }
    .card {
      padding: 22px;
    }
    .artist-hero img,
    .artist-hero .ph {
      width: 120px;
      height: 120px;
    }
    .song-hero img,
    .song-hero .square {
      width: 140px;
      height: 140px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
    .bars {
      height: 140px;
    }
  }
</style>
