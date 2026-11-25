<script>
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n.js';
  import { RECORD_TYPES } from '../../uploads.js';
  import ArtistPicker from '../upload/ArtistPicker.svelte';
  import Segmented from '../Segmented.svelte';

  export let title = '';
  export let artists = [];
  export let artistQuery = '';
  export let recordType = 'album';
  export let releaseDate = '';
  export let required = false;
  export let typeAuto = false;
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
      <span class="label">{$t('upload.type')}{#if required}{' '}<b class="req">*</b>{/if}</span>
      <Segmented
        options={RECORD_TYPES.map((type) => [type, $t(`type.${type}`)])}
        value={recordType}
        label={$t('upload.type')}
        on:change={(e) => ((recordType = e.detail), dispatch('type'))}
      />
      {#if typeAuto}<small>{$t('upload.typeAuto')}</small>{/if}
    </div>
    <label class="field">
      <span class="label">{$t('upload.date')}{#if required}{' '}<b class="req">*</b>{/if}</span>
      <input type="date" class:invalid={bad('release_date')} bind:value={releaseDate} on:input={() => dispatch('date')} />
    </label>
  </div>
</section>
