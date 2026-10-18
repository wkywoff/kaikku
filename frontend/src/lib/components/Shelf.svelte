<script>
  // Полка на главной: заголовок и один ряд карточек (сколько помещается по
  // ширине) с кнопкой «Показать все», как у Spotify. Карточку рисует
  // страница: <Shelf {items} let:item>…</Shelf>.
  import { MAX_PER_ROW } from '../layout.js';
  import { t } from '../i18n.js';

  export let title;
  export let subtitle = '';
  export let items = [];
  export let key = (item) => item.id;
  // loading — данные ещё идут: заголовок и ряд серых карточек на их месте
  // (главная); round — круглые, как у артистов.
  export let loading = false;
  export let round = false;
  // priority — полка первая на странице: её первый ряд виден сразу, картинки
  // грузятся без отложенной загрузки (слот получает eager, первая — first).
  export let priority = false;

  let expanded = false;
  let width = 0;

  // Сколько карточек в ряду сетки (min 176 px; на телефоне — 2).
  $: perRow = width < 560 ? 2 : Math.min(MAX_PER_ROW, Math.max(2, Math.floor(width / 176)));
  $: visible = expanded ? items : items.slice(0, perRow);
</script>

{#if loading}
  <section aria-busy="true">
    <div class="head"><h2>{title}</h2></div>
    <div class="grid" bind:clientWidth={width}>
      {#each Array(perRow) as _}
        <div class="ghost" aria-hidden="true">
          <div class="ghost-art" class:round></div>
          <div class="ghost-line"></div>
          <div class="ghost-line short"></div>
        </div>
      {/each}
    </div>
  </section>
{:else if items.length}
  <section>
    <div class="head">
      <div>
        <h2>{title}</h2>
        {#if subtitle}<p class="sub">{subtitle}</p>{/if}
      </div>
      {#if items.length > perRow}
        <button class="show-all" on:click={() => (expanded = !expanded)}>
          {expanded ? $t('disc.showLess') : $t('disc.showAll')}
        </button>
      {/if}
    </div>
    <div class="grid" bind:clientWidth={width}>
      {#each visible as item, i (key(item))}
        <slot {item} eager={priority && i < perRow} first={priority && i === 0} />
      {/each}
    </div>
  </section>
{/if}

<style>
  section {
    margin-top: 32px;
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 8px;
  }
  h2 {
    font-size: 24px;
  }
  .sub {
    margin-top: 2px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .show-all {
    flex-shrink: 0;
    font-size: 14px;
    font-weight: 700;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .show-all:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  /* Заглушка карточки — те же размеры, что у MediaCard. «Дышит» только
     прозрачностью: её браузер анимирует без перерисовки. */
  .ghost {
    padding: 12px;
    min-width: 0;
    animation: ghost 1.6s ease-in-out infinite;
  }
  .ghost-art {
    aspect-ratio: 1;
    margin-bottom: 10px;
    border-radius: 6px;
    background: var(--bg-card);
  }
  .ghost-art.round {
    border-radius: 50%;
  }
  .ghost-line {
    width: 70%;
    height: 12px;
    margin: 5px 0;
    border-radius: 6px;
    background: var(--bg-card);
  }
  .ghost-line.short {
    width: 45%;
  }
  @keyframes ghost {
    50% {
      opacity: 0.55;
    }
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
