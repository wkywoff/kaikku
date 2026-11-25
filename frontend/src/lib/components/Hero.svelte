<script>
  import { sized } from '../imageproxy.js';
  import { createEventDispatcher, onMount } from 'svelte';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import UserRound from '@lucide/svelte/icons/user-round';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import { t } from '../i18n.js';
  import { fitFontSize } from '../fitTitle.js';

  export let image = null;
  export let round = false;
  export let kind = '';
  export let title = '';
  export let zoomLabel = '';
  export let hidden = false;

  const dispatch = createEventDispatcher();

  const PROBE_PX = 100;
  const MARK_EM = 0.9;

  let ruler;
  let probe;
  let heading;
  let fontSize = 0;

  function fit(mark = hidden) {
    if (!ruler) return;
    const cs = getComputedStyle(heading);
    const unit = probe.getBoundingClientRect().width / PROBE_PX + (mark ? MARK_EM : 0);
    fontSize = fitFontSize(
      ruler.clientWidth,
      unit,
      parseFloat(cs.getPropertyValue('--fit-min')),
      parseFloat(cs.getPropertyValue('--fit-max')),
    );
  }

  $: fit(hidden);

  onMount(() => {
    const ro = new ResizeObserver(() => fit());
    ro.observe(ruler);
    ro.observe(probe);
    fit();
    return () => ro.disconnect();
  });
</script>

<header class="hero">
  <div class="art" class:round>
    {#if $$slots.art}
      <slot name="art" />
    {:else if image && zoomLabel}
      <button class="zoom" on:click={() => dispatch('zoom')} aria-label={zoomLabel} title={zoomLabel}>
        <img src={sized(image, round ? 320 : 300)} alt="" />
      </button>
    {:else if image}
      <img src={sized(image, round ? 320 : 300)} alt="" />
    {:else}
      <div class="placeholder">
        {#if round}<UserRound size={72} />{:else}<Disc3 size={72} />{/if}
      </div>
    {/if}
  </div>
  <div class="info">
    {#if kind}<span class="kind">{kind}</span>{/if}
    <div class="ruler" bind:this={ruler} aria-hidden="true"><span class="probe" bind:this={probe}>{title}</span></div>
    <h1 bind:this={heading} style:font-size={fontSize ? `${fontSize}px` : null}>{title}{#if hidden}<span class="hidden-mark" title={$t('admin.hiddenRelease')} aria-label={$t('admin.hiddenRelease')}><EyeOff /></span>{/if}</h1>
    <div class="meta"><slot name="meta" /></div>
  </div>
</header>

<style>
  .hero {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    padding: 24px 0 24px;
    min-height: 280px;
  }
  .art {
    width: 232px;
    height: 232px;
    flex-shrink: 0;
    border-radius: 6px;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.28);
  }
  .art.round {
    border-radius: 50%;
  }
  .art img,
  .placeholder {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
    display: block;
  }
  .zoom {
    display: block;
    width: 100%;
    height: 100%;
    border-radius: inherit;
    cursor: zoom-in;
    transition: transform 0.15s;
  }
  @media (hover: hover) {
    .zoom:hover {
      transform: scale(1.015);
    }
  }
  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .info {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }
  .kind {
    font-size: 14px;
    font-weight: 500;
  }
  h1 {
    --fit-min: 36px;
    --fit-max: 80px;
    font-size: var(--fit-min);
    line-height: 1.05;
    margin: 4px 0 10px;
    overflow-wrap: anywhere;
  }
  h1,
  .probe {
    font-family: var(--font-display);
    font-weight: 800;
    letter-spacing: -0.03em;
  }
  .ruler {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    height: 0;
    overflow: hidden;
    visibility: hidden;
    pointer-events: none;
  }
  .probe {
    position: absolute;
    left: 0;
    top: 0;
    font-size: 100px;
    white-space: nowrap;
  }
  .hidden-mark {
    display: inline-flex;
    vertical-align: middle;
    margin-left: 0.3em;
    color: rgba(255, 255, 255, 0.6);
  }
  .hidden-mark :global(svg) {
    width: 0.6em;
    height: 0.6em;
  }
  .meta {
    font-size: 14px;
    color: rgba(255, 255, 255, 0.85);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }

  @media (max-width: 768px) {
    .hero {
      flex-direction: column;
      align-items: center;
      text-align: center;
      min-height: 0;
      gap: 16px;
      padding-top: 12px;
    }
    .art {
      width: min(62vw, 240px);
      height: min(62vw, 240px);
    }
    .info {
      flex: none;
      width: 100%;
      align-items: center;
    }
    h1 {
      --fit-min: 24px;
      --fit-max: 34px;
    }
    .meta {
      justify-content: center;
    }
  }
</style>
