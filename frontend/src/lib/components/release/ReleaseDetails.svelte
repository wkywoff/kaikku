<script>
  // Раздел «О релизе» на странице правки (EditShell): название, исполнители
  // (ArtistPicker — строками, порядок перетаскиванием), тип и дата. Одинаков
  // у своего релиза и релиза Deezer. invalid — коды того, чего не хватает
  // для публикации (lib/uploads.js), их поля подсвечиваются.
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n.js';
  import { RECORD_TYPES } from '../../uploads.js';
  import ArtistPicker from '../upload/ArtistPicker.svelte';

  export let title = '';
  export let artists = [];
  export let artistQuery = '';
  export let recordType = 'album';
  export let releaseDate = '';
  export let required = false; // своему релизу для публикации нужно всё: звёздочки
  export let typeAuto = false; // тип подбирается по трекам, пока его не выбрали
  export let invalid = [];

  const dispatch = createEventDispatcher();
  $: bad = (code) => invalid.includes(code);
</script>

<section class="card">
  <h2>{$t('upload.details')}</h2>
  <label class="field">
    <span class="label">{$t('upload.releaseTitle')}{#if required}{' '}<b class="req">*</b>{/if}</span>
    <input type="text" maxlength="300" class:invalid={bad('title')} placeholder={$t('upload.titlePlaceholder')} bind:value={title} on:input={() => dispatch('title')} />
  </label>
  <div class="field">
    <label class="label" for="release-artists">{$t('upload.artists')}{#if required}{' '}<b class="req">*</b>{/if}</label>
    <ArtistPicker bind:artists bind:query={artistQuery} invalid={bad('artists')} inputId="release-artists" />
    <small>{$t('upload.artistsHint')}</small>
  </div>
  <div class="row2">
    <div class="field">
      <span class="label" id="release-type">{$t('upload.type')}{#if required}{' '}<b class="req">*</b>{/if}</span>
      <div class="segmented" role="radiogroup" aria-labelledby="release-type">
        {#each RECORD_TYPES as type}
          <button type="button" role="radio" aria-checked={recordType === type} class:active={recordType === type}
            on:click={() => ((recordType = type), dispatch('type'))}>{$t(`type.${type}`)}</button>
        {/each}
      </div>
      {#if typeAuto}<small>{$t('upload.typeAuto')}</small>{/if}
    </div>
    <label class="field">
      <span class="label">{$t('upload.date')}{#if required}{' '}<b class="req">*</b>{/if}</span>
      <input type="date" class:invalid={bad('release_date')} bind:value={releaseDate} on:input={() => dispatch('date')} />
    </label>
  </div>
</section>

<style>
  .segmented {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 4px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.25);
    width: fit-content;
  }
  .segmented button {
    padding: 7px 14px;
    border-radius: 999px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .segmented button.active {
    background: var(--text);
    color: var(--bg);
  }
  /* «Мини-альбом» длинный: четыре типа должны влезть в одну строку */
  @media (max-width: 720px) {
    .segmented button {
      padding: 6px 10px;
      font-size: 13px;
    }
  }
</style>
