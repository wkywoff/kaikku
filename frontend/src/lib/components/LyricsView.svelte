<script context="module">
  import { LRU } from '../lru.js';
  const cache = new LRU(30); // deezer id -> Promise<lyrics | null>

  /** Текст трека нашли заново (админ, окно правки) — при следующем показе
   * загрузить с сервера. */
  export function forgetLyrics(id) {
    cache.delete(id);
  }
</script>

<script>
  // Текст песни в стиле Apple Music / Beautiful Lyrics. Сервер отдаёт строки
  // с таймингом по словам, по строкам или без времени — что нашлось у
  // Deezer, NetEase или LRCLIB (internal/lyrics).
  //   - текущая строка яркая; слова и слоги заливаются в такт, спетые чуть
  //     приподнимаются; подпевки — мельче под строкой;
  //   - в проигрыше дольше 3 с — три «дышащие» точки;
  //   - текст сам прокручивается к текущей строке; если листают руками —
  //     ждёт 3 с; нажатие на строку перематывает к ней.
  // Позиция трека — player.position() на каждом кадре (стор плеера
  // обновляется только 4 раза в секунду), а заливка меняется прямо в DOM
  // только у текущей строки — без перерисовки Svelte.
  import { onMount, tick } from 'svelte';
  import { api } from '../api.js';
  import { player, isPlaying } from '../stores/player.js';
  import { t } from '../i18n.js';

  export let deezerId = null;

  let state = 'loading'; // loading | ready | none | error
  let lyrics = null;
  let box;
  let active = -1; // индекс текущей строки
  let gap = -1; // индекс строки, перед которой сейчас проигрыш с точками

  const SOURCES = { deezer: 'Deezer', netease: 'NetEase Cloud Music', lrclib: 'LRCLIB' };
  const LEAD = 0.12; // с; строка загорается чуть раньше голоса, как в Apple Music
  const GAP_MIN = 3; // с; короче — без точек
  const ACTIVE_AT = 0.22; // доля высоты сверху, где стоит текущая строка, как в Apple Music

  $: load(deezerId);
  async function load(id) {
    state = 'loading';
    lyrics = null;
    active = gap = -1;
    if (!id) {
      state = 'none';
      return;
    }
    // Источник текста бывает перегружен — пара повторов с паузой, а не
    // сразу «не удалось».
    for (let attempt = 0; ; attempt++) {
      try {
        const l = await fetchLyrics(id);
        if (id !== deezerId) return;
        lyrics = l;
        state = l?.lines?.length ? 'ready' : 'none';
        await tick();
        box?.scrollTo({ top: 0 });
        return;
      } catch {
        if (id !== deezerId) return;
        if (attempt === 2) {
          state = 'error';
          return;
        }
        await new Promise((r) => setTimeout(r, 1500 * (attempt + 1)));
        if (id !== deezerId) return;
      }
    }
  }

  function fetchLyrics(id) {
    if (!cache.has(id)) {
      cache.set(
        id,
        api.lyrics(id).then(
          (r) => r.lyrics,
          (e) => {
            cache.delete(id);
            throw e;
          },
        ),
      );
    }
    return cache.get(id);
  }

  $: synced = lyrics && lyrics.sync !== 'none';
  $: lines = lyrics?.lines || [];

  // Проигрыш перед строкой i: от конца прошлой (или начала трека) до её начала.
  function gapBefore(i) {
    if (!synced) return false;
    const prevEnd = i === 0 ? 0 : lines[i - 1].e;
    return lines[i].s - prevEnd >= GAP_MIN;
  }

  // --- автопрокрутка ---
  let userScrollAt = 0;
  const onUserScroll = () => (userScrollAt = performance.now());

  function scrollToActive(smooth = true) {
    if (!box || active < 0 || performance.now() - userScrollAt < 3000) return;
    const el = box.querySelector(`[data-line="${gap >= 0 ? `gap-${gap}` : active}"]`);
    if (!el) return;
    // Проигрыш перед строкой только что кончился: точки над ней ещё
    // сворачиваются (0.4 с) и строка поднимется на их место. Считать от
    // него — иначе после сворачивания строку утягивало выше нужного.
    const dots = gap < 0 && box.querySelector(`[data-line="gap-${active}"]`);
    const top = (dots ? dots.offsetTop : el.offsetTop) - box.clientHeight * ACTIVE_AT;
    box.scrollTo({ top: Math.max(0, top), behavior: smooth ? 'smooth' : 'auto' });
  }

  function seekTo(line) {
    if (!synced) return;
    userScrollAt = 0;
    player.seek(Math.max(0, line.s - 0.05));
    if (!$isPlaying) player.toggle();
  }

  // --- кадр: какая строка, заливка слов ---
  let lit = null; // элемент строки, у слов которой стоит --p

  function findActive(time) {
    // Последняя начавшаяся строка (двоичный поиск по началу).
    let lo = 0;
    let hi = lines.length - 1;
    let found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (lines[mid].s <= time) {
        found = mid;
        lo = mid + 1;
      } else hi = mid - 1;
    }
    return found;
  }

  function paintWords(lineEl, line, time) {
    const spans = lineEl.querySelectorAll('.w');
    const words = [...(line.w || []), ...(line.bg || [])];
    spans.forEach((span, k) => {
      const w = words[k];
      if (!w) return;
      const p = w.e > w.s ? Math.min(1, Math.max(0, (time - w.s) / (w.e - w.s))) : time >= w.s ? 1 : 0;
      span.style.setProperty('--p', p.toFixed(3));
    });
  }

  function clearWords(lineEl) {
    lineEl?.querySelectorAll('.w').forEach((s) => s.style.removeProperty('--p'));
  }

  function frame() {
    if (state !== 'ready' || !synced || !box) return;
    const time = player.position() + LEAD;
    let a = findActive(time);
    let g = -1;
    // Проигрыш: прошлая строка кончилась, следующая не началась.
    const next = a + 1;
    if (next < lines.length && gapBefore(next)) {
      const prevEnd = a < 0 ? 0 : lines[a].e;
      if (time >= prevEnd) g = next;
    }
    if (a !== active || g !== gap) {
      active = a;
      gap = g;
      scrollToActive();
    }
    // Точки проигрыша заполняются по ходу паузы.
    if (g >= 0) {
      const dots = box.querySelector(`[data-line="gap-${g}"]`);
      const start = g === 0 ? 0 : lines[g - 1].e;
      dots?.style.setProperty('--p', Math.min(1, (time - start) / (lines[g].s - start)).toFixed(3));
    }
    const el = a >= 0 ? box.querySelector(`[data-line="${a}"]`) : null;
    if (el !== lit) {
      clearWords(lit);
      lit = el;
    }
    if (el && (lines[a].w?.length || lines[a].bg?.length)) paintWords(el, lines[a], time);
  }

  onMount(() => {
    let raf = 0;
    const loop = () => {
      frame();
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    // Другой размер (поворот телефона, окно) — строки переносятся иначе,
    // текущая уезжает: вернуть её на место.
    const ro = new ResizeObserver(() => scrollToActive(false));
    ro.observe(box);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  });

  // Новый текст — сразу к текущему месту, без плавной прокрутки.
  $: if (state === 'ready') tick().then(() => scrollToActive(false));
</script>

<div
  class="lyrics"
  class:static={!synced}
  bind:this={box}
  on:wheel={onUserScroll}
  on:touchmove={onUserScroll}
  role="region"
  aria-label={$t('lyrics.title')}
>
  {#if state === 'loading'}
    <p class="note">{$t('lyrics.loading')}</p>
  {:else if state === 'none'}
    <p class="note">{$t('lyrics.none')}</p>
  {:else if state === 'error'}
    <p class="note">{$t('lyrics.error')}</p>
  {:else}
    <div class="pad-top"></div>
    {#each lines as line, i}
      {#if gapBefore(i)}
        <div class="interlude" class:on={gap === i} class:paused={!$isPlaying} data-line="gap-{i}" aria-hidden="true"><i></i><i></i><i></i></div>
      {/if}
      {#if line.t || line.bg?.length}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
        <p
          class="line"
          class:active={synced && i === active && gap < 0}
          class:past={synced && (i < active || (i === active && gap >= 0))}
          class:right={line.r}
          class:words={line.w?.length}
          class:clickable={synced}
          data-line={i}
          style="--d: {synced ? Math.min(4, Math.abs(i - active)) : 0}"
          on:click={() => seekTo(line)}
        >
          {#if line.w?.length}{#each line.w as w}<span class="w">{w.t}</span>{w.g ? '' : ' '}{/each}{:else}{line.t}{/if}
          {#if line.bg?.length}<span class="bg">{#each line.bg as w}<span class="w">{w.t}</span>{w.g ? '' : ' '}{/each}</span>{/if}
        </p>
      {:else}
        <div class="stanza"></div>
      {/if}
    {/each}
    <div class="credit">
      {#if lyrics.writers}<p>{$t('lyrics.writers', { names: lyrics.writers })}</p>{/if}
      <p>{$t('lyrics.source', { source: SOURCES[lyrics.source] || lyrics.source })}</p>
    </div>
    <div class="pad-bottom"></div>
  {/if}
</div>

<style>
  /* relative — чтобы offsetTop строк считался от самого текста (см.
     scrollToActive), а не от плеера: иначе строка вставала выше нужного
     на высоту всего, что над текстом. */
  .lyrics {
    position: relative;
    height: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
    /* Низ растворяется; --lyrics-cut — сколько снизу закрыто кнопками
       плеера (NowPlaying, телефон), там текста не видно. */
    -webkit-mask-image: linear-gradient(180deg, transparent 0, #000 12%, #000 calc(82% - var(--lyrics-cut)), transparent calc(100% - var(--lyrics-cut)));
    mask-image: linear-gradient(180deg, transparent 0, #000 12%, #000 calc(82% - var(--lyrics-cut)), transparent calc(100% - var(--lyrics-cut)));
  }
  .lyrics::-webkit-scrollbar {
    display: none;
  }
  .pad-top {
    height: 28%;
  }
  .pad-bottom {
    height: 50%;
  }
  .note {
    padding: 30% 8px 0;
    color: rgba(255, 255, 255, 0.6);
    font-size: 18px;
    text-align: center;
  }
  /* Справа запас 4%: текущая строка увеличивается на 3% от левого края и
     иначе уходила бы последним словом за край колонки. */
  .line {
    --p: 0;
    margin: 0 4% 22px 0;
    padding: 4px 8px;
    font-size: clamp(24px, 2.6vw, 38px);
    font-weight: 800;
    line-height: 1.25;
    letter-spacing: -0.01em;
    color: rgba(255, 255, 255, 0.32);
    transform-origin: left center;
    transition:
      color 0.35s,
      transform 0.35s,
      opacity 0.35s,
      filter 0.35s;
    border-radius: 8px;
    overflow-wrap: anywhere;
  }
  .line.right {
    margin-right: 0;
    margin-left: 4%;
    text-align: right;
    transform-origin: right center;
  }
  .line.clickable {
    cursor: pointer;
  }
  .line.past {
    color: rgba(255, 255, 255, 0.5);
  }
  .line.active {
    color: #fff;
    transform: scale(1.03);
  }
  /* Строка с таймингом по словам: слова заливаются сами (--p ставит JS у
     текущей строки), а цвет строки не нужен. */
  .line.words.active {
    color: transparent;
  }
  .w {
    display: inline-block;
    white-space: pre;
  }
  .line.words.active .w {
    background-image: linear-gradient(
      90deg,
      #fff 0%,
      #fff calc(var(--p) * 130% - 30%),
      rgba(255, 255, 255, 0.35) calc(var(--p) * 130%),
      rgba(255, 255, 255, 0.35) 100%
    );
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
    transform: translateY(calc(var(--p) * -2px));
    transition: transform 0.25s;
  }
  .line.words.past .w {
    color: rgba(255, 255, 255, 0.5);
  }
  .bg {
    display: block;
    margin-top: 4px;
    font-size: 0.62em;
    font-weight: 700;
    opacity: 0.85;
  }
  .stanza {
    height: 18px;
  }
  /* Проигрыш: три точки «дышат» и по очереди загораются (--p — доля паузы). */
  /* Ширина — по точкам, а не на всю колонку: «дыхание» масштабирует от
     левого края, и крайняя точка не уходит за край. На паузе замирает. */
  .interlude {
    --p: 0;
    display: flex;
    width: max-content;
    transform-origin: left center;
    gap: 10px;
    height: 0;
    margin: 0 8px;
    overflow: hidden;
    opacity: 0;
    transition:
      height 0.4s,
      opacity 0.4s,
      margin 0.4s;
  }
  .interlude.on {
    height: 28px;
    margin: 8px 8px 26px;
    opacity: 1;
    animation: breathe 2.4s ease-in-out infinite;
  }
  .interlude.paused {
    animation-play-state: paused;
  }
  .interlude i {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: rgba(255, 255, 255, 0.3);
    transition: background-color 0.3s;
  }
  .interlude i:nth-child(1) {
    background: rgba(255, 255, 255, calc(0.3 + 0.7 * min(1, var(--p) * 3)));
  }
  .interlude i:nth-child(2) {
    background: rgba(255, 255, 255, calc(0.3 + 0.7 * clamp(0, var(--p) * 3 - 1, 1)));
  }
  .interlude i:nth-child(3) {
    background: rgba(255, 255, 255, calc(0.3 + 0.7 * clamp(0, var(--p) * 3 - 2, 1)));
  }
  @keyframes breathe {
    0%,
    100% {
      transform: scale(0.92);
    }
    50% {
      transform: scale(1.08);
    }
  }
  .static .line {
    color: rgba(255, 255, 255, 0.9);
    font-size: clamp(20px, 2vw, 28px);
    margin-bottom: 10px;
  }
  .credit {
    margin-top: 32px;
    padding: 0 8px;
    font-size: 13px;
    color: rgba(255, 255, 255, 0.55);
  }
  /* На компьютере дальние строки чуть размыты, как в Apple Music; на
     телефоне без размытия — дорого для видеокарты при прокрутке. */
  @media (hover: hover) and (pointer: fine) {
    .line {
      filter: blur(calc(var(--d) * 0.5px));
    }
    /* Подсветка под курсором — только с мышью: после касания на телефоне
       «наведение» залипает, и строка оставалась подсвеченной. */
    .line.clickable:hover {
      background: rgba(255, 255, 255, 0.06);
    }
    .line:hover,
    .static .line {
      filter: none;
    }
  }
  @media (max-width: 900px) {
    .line {
      font-size: 26px;
      margin-bottom: 16px;
    }
  }
</style>
