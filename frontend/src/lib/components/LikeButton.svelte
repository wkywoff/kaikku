<script>
  // «В любимые» для играющего трека (плеер, полноэкранный плеер): плюс,
  // а если трек уже там — зелёная галочка. Без Deezer ID (старые записи
  // медиатеки) лайкнуть нельзя — кнопки нет.
  import { t } from '../i18n.js';
  import { likedIds, toggleLike } from '../stores/collection.js';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CirclePlus from '@lucide/svelte/icons/circle-plus';

  export let deezerId = null;
  export let size = 18;

  $: liked = !!deezerId && $likedIds.has(deezerId);
</script>

{#if deezerId}
  <button
    class="icon-btn like"
    class:on={liked}
    title={$t(liked ? 'menu.unlike' : 'menu.like')}
    aria-label={$t(liked ? 'menu.unlike' : 'menu.like')}
    aria-pressed={liked}
    on:click|stopPropagation={() => toggleLike(deezerId)}
  >
    {#if liked}<CircleCheck {size} />{:else}<CirclePlus {size} />{/if}
  </button>
{/if}

<style>
  .like {
    flex-shrink: 0;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .like:hover {
      color: var(--text);
    }
  }
  .like.on {
    color: var(--ok);
  }
</style>
