<script>
  // Профиль человека (/user/<ник>): подписка и публичные плейлисты.
  // Подписаны друг на друга — друзья: их зовут в соавторы плейлиста
  // (PlaylistDialog). В своём профиле — люди вокруг: друзья, кто подписан
  // на тебя (подписаться в ответ) и на кого подписан ты.
  import { api } from '../api.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { setRouteTitle } from '../stores/title.js';
  import { toast } from '../stores/toast.js';
  import { playPlaylist, playingContext, isContext } from '../library.js';
  import { playlistMenuItems } from '../cardMenu.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { longpress } from '../longpress.js';
  import { linkItems } from '../share.js';
  import { mixHue, mixColor } from '../mixes.js';
  import { tracksCount } from '../format.js';
  import Hero from '../components/Hero.svelte';
  import Artwork from '../components/Artwork.svelte';
  import MediaCard from '../components/MediaCard.svelte';
  import Shelf from '../components/Shelf.svelte';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';

  const name = $navStore.id;
  let page = null; // { user, me, playlists, people? }
  let missing = false;
  let error = '';
  let busy = false;

  pageTint().color(mixColor(`user-${name.toLowerCase()}`));

  api
    .profile(name)
    .then((p) => (page = p))
    .catch((e) => (e.status === 404 ? (missing = true) : (error = e.message)));

  $: user = page?.user;
  $: if (user) setRouteTitle(`user:${name}`, user.username);
  $: friends = (page?.people || []).filter((p) => p.i_follow && p.follows_me);
  $: followers = (page?.people || []).filter((p) => p.follows_me && !p.i_follow);
  $: following = (page?.people || []).filter((p) => p.i_follow && !p.follows_me);
  $: playingCtx = $playerPlaying ? $playingContext : null;
  $: hue = mixHue(`user-${name.toLowerCase()}`);

  // Подписаться / отписаться — на профиле и в списках своего профиля.
  async function toggle(p) {
    busy = true;
    try {
      const fresh = await (p.i_follow ? api.unfollowUser(p.id) : api.followUser(p.id));
      if (page.user.id === fresh.id) page.user = fresh;
      if (page.people) page.people = page.people.map((x) => (x.id === fresh.id ? fresh : x));
      page = page;
      if (fresh.i_follow && fresh.follows_me) toast($t('people.nowFriends', { name: fresh.username }));
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      busy = false;
    }
  }

  function menu(e) {
    openMenu(e, linkItems(paths.user(user.username), user.username));
  }

  const followLabel = (p) => (p.i_follow ? (p.follows_me ? 'people.friends' : 'people.following') : p.follows_me ? 'people.followBack' : 'people.follow');
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if missing}
  <div class="page-pad empty">
    <h2>{$t('people.missing')}</h2>
  </div>
{:else if page}
  <div class="page-pad" role="group" on:contextmenu={menu} use:longpress={menu}>
    <Hero kind={$t('people.kind')} title={user.username} round>
      <div slot="art" class="initial" style="--hue: {hue}" aria-hidden="true">{user.username.slice(0, 1).toUpperCase()}</div>
      <svelte:fragment slot="meta">
        {#if page.me}
          {$t('people.counts', { friends: friends.length, followers: followers.length + friends.length })}
        {:else if user.i_follow && user.follows_me}
          {$t('people.friendsHint')}
        {:else if user.follows_me}
          {$t('people.followsYou')}
        {/if}
      </svelte:fragment>
    </Hero>
  </div>

  <div class="actions page-pad">
    {#if !page.me}
      <button class="pill follow" class:outline={user.i_follow} disabled={busy} aria-pressed={user.i_follow} on:click={() => toggle(user)}>{$t(followLabel(user))}</button>
    {/if}
    <button class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: user.username })} on:click={menu}><Ellipsis size={24} /></button>
  </div>

  {#if page.me}
    {#each [['people.friendsTitle', friends], ['people.followersTitle', followers], ['people.followingTitle', following]] as [title, list] (title)}
      {#if list.length}
        <section class="page-pad">
          <h2>{$t(title)}</h2>
          <ul class="people">
            {#each list as p (p.id)}
              <li>
                <a class="person" href={paths.user(p.username)} use:link>
                  <span class="dot" style="--hue: {mixHue(`user-${p.username.toLowerCase()}`)}">{p.username.slice(0, 1).toUpperCase()}</span>
                  <span class="pname">{p.username}</span>
                </a>
                <button class="pill small" class:outline={p.i_follow} disabled={busy} on:click={() => toggle(p)}>{$t(followLabel(p))}</button>
              </li>
            {/each}
          </ul>
        </section>
      {/if}
    {/each}
    {#if !page.people.length}
      <p class="page-pad muted hint">{$t('people.empty')}</p>
    {/if}
  {/if}

  {#if page.playlists.length}
    <div class="page-pad">
      <Shelf title={$t('people.playlists')} items={page.playlists} let:item>
        <MediaCard
          href={paths.playlist(item.id)}
          menu={() => playlistMenuItems(item)}
          title={item.title}
          subtitle={`${$t('playlist.kind')} • ${tracksCount(item.track_count, $lang)}`}
          onPlay={item.track_count ? () => playPlaylist(item.id) : null}
          playing={isContext(playingCtx, 'playlist', item.id)}
        >
          <Artwork slot="art" images={item.covers} iconSize={48} />
        </MediaCard>
      </Shelf>
    </div>
  {:else}
    <section class="page-pad">
      <h2>{$t('people.playlists')}</h2>
      <p class="muted">{$t(page.me ? 'people.noPlaylistsMe' : 'people.noPlaylists')}</p>
    </section>
  {/if}
{/if}

<style>
  .initial {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: hsl(var(--hue) 45% 38%);
    color: #fff;
    font-size: 96px;
    font-weight: 800;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 16px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  .follow {
    padding: 0 16px;
    font-weight: 700;
  }
  section {
    margin-bottom: 32px;
  }
  section h2 {
    font-size: 24px;
    margin-bottom: 12px;
  }
  .people {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-width: 560px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .people li {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .person {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 8px;
    border-radius: 6px;
  }
  @media (hover: hover) {
    .person:hover {
      background: var(--bg-hover);
    }
  }
  .dot {
    width: 40px;
    height: 40px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: hsl(var(--hue) 45% 38%);
    color: #fff;
    font-weight: 800;
  }
  .pname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
  .pill.small {
    height: 32px;
    padding: 0 12px;
    font-size: 13px;
    flex-shrink: 0;
  }
  .hint {
    max-width: 560px;
    margin-bottom: 32px;
  }
  .empty {
    padding-top: 48px;
  }
  @media (max-width: 768px) {
    .initial {
      font-size: 72px;
    }
    section h2 {
      font-size: 20px;
    }
  }
</style>
