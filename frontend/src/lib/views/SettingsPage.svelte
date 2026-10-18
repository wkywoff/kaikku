<script>
  // Настройки (на этом устройстве): язык, переход между треками,
  // выравнивание громкости, вид треклистов. Применяются сразу.
  import { settings } from '../stores/settings.js';
  import { auth } from '../stores/auth.js';
  import { signOut } from '../signout.js';
  import { pageTint } from '../stores/theme.js';
  import { t } from '../i18n.js';
  import LogOut from '@lucide/svelte/icons/log-out';
  import { api } from '../api.js';
  import { toast } from '../stores/toast.js';
  import { me as meStore } from '../stores/me.js';
  import { directAudio } from '../stores/player.js';
  import { navigate, paths } from '../stores/nav.js';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import Copy from '@lucide/svelte/icons/copy';

  export let me = null;

  pageTint().from(null);

  // Версия сервера — внизу страницы, для сообщений об ошибках. 1.2.0 → v1.2.0.
  let version = '';
  api.health().then((h) => (version = /^\d/.test(h.version) ? `v${h.version}` : h.version)).catch(() => {});

  // Смена пароля: после неё остальные устройства выходят, это — получает новый вход.
  let pwOpen = false;
  let pwOld = '';
  let pwNew = '';
  let pwNew2 = '';
  let pwBusy = false;
  let pwError = '';

  async function changePassword() {
    pwError = '';
    if (pwNew.length < 8 || pwNew.length > 256) return (pwError = $t('login.invalid'));
    if (pwNew !== pwNew2) return (pwError = $t('login.mismatch'));
    pwBusy = true;
    try {
      const { token } = await api.changePassword(pwOld, pwNew);
      auth.login(token);
      pwOpen = false;
      pwOld = pwNew = pwNew2 = '';
      toast($t('settings.passwordChanged'));
    } catch (e) {
      pwError = e.status === 403 ? $t('settings.wrongPassword') : e.status === 429 ? $t('login.tooMany') : e.message;
    } finally {
      pwBusy = false;
    }
  }

  // Last.fm: «Подключить» уводит на last.fm, тот возвращает на /lastfm?token=…
  // (та же страница настроек) — токен меняем на привязку и убираем из адреса.
  let lastfm = null; // { available, auth_url, username?, scrobble?, love?, pending? }
  let lastfmBusy = false;

  async function loadLastfm() {
    try {
      lastfm = await api.lastfm(`${location.origin}/lastfm`);
    } catch {
      lastfm = null;
    }
  }

  function applyLastfm(st) {
    lastfm = { ...st, auth_url: st.auth_url || lastfm?.auth_url };
    meStore.update((m) => m && { ...m, lastfm_scrobble: !!st.username && !!st.scrobble });
  }

  async function lastfmAction(fn, done) {
    lastfmBusy = true;
    try {
      await fn();
      if (done) toast(done);
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      lastfmBusy = false;
    }
  }

  const linkLastfm = (token) =>
    lastfmAction(async () => {
      try {
        applyLastfm(await api.lastfmLink(token));
      } catch (e) {
        throw e.status === 400 ? new Error($t('lastfm.badToken')) : e;
      }
    }, $t('lastfm.linked'));

  const setLastfm = (patch) => lastfmAction(async () => applyLastfm(await api.lastfmSettings(patch)));

  const unlinkLastfm = () =>
    lastfmAction(async () => {
      await api.lastfmUnlink();
      applyLastfm({ ...lastfm, username: null });
    }, $t('lastfm.unlinked'));

  if (location.pathname === '/lastfm') {
    const token = new URLSearchParams(location.search).get('token');
    navigate(paths.settings(), { replace: true });
    loadLastfm().then(() => token && linkLastfm(token));
  } else {
    loadLastfm();
  }

  // Приложения (Subsonic API): свой пароль, показывается здесь же.
  let apps = null; // { enabled, username, password? }
  let appsBusy = false;
  api.subsonic().then((a) => (apps = a), () => {});

  async function appsAction(fn) {
    appsBusy = true;
    try {
      apps = await fn();
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      appsBusy = false;
    }
  }
  const newAppPassword = () => appsAction(() => api.newSubsonicPassword());
  const disableApps = () => appsAction(async () => (await api.disableSubsonic(), { enabled: false, username: apps.username }));

  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
      toast($t('apps.copied'));
    } catch {
      // видно на экране — можно выделить руками
    }
  }

  const languages = [
    ['ru', 'Русский'],
    ['en', 'English'],
  ];
</script>

<div class="page-pad settings">
  <h1>{$t('settings.title')}</h1>

  <section>
    <h2>{$t('settings.language')}</h2>
    <div class="row">
      <div class="label">
        <span>{$t('settings.languageHint')}</span>
      </div>
      <div class="segmented" role="radiogroup" aria-label={$t('settings.language')}>
        {#each languages as [code, name]}
          <button role="radio" aria-checked={$settings.lang === code} class:active={$settings.lang === code} on:click={() => settings.set('lang', code)}>{name}</button>
        {/each}
      </div>
    </div>
  </section>

  <section>
    <h2>{$t('settings.playback')}</h2>
    <!-- На iPhone звук идёт без Web Audio (stores/player.js, directAudio):
         перехода и выравнивания там нет. -->
    {#if directAudio}<p class="note">{$t('settings.iosAudio')}</p>{/if}
    <div class="row" class:disabled={directAudio}>
      <div class="label">
        <span>{$t('settings.crossfade')}</span>
        <small>{$t('settings.crossfadeHint')}</small>
      </div>
      <button class="switch" role="switch" aria-checked={$settings.crossfade && !directAudio} aria-label={$t('settings.crossfade')} disabled={directAudio} on:click={() => settings.set('crossfade', !$settings.crossfade)}>
        <span class="knob"></span>
      </button>
    </div>
    <div class="row" class:disabled={!$settings.crossfade || directAudio}>
      <div class="label"><span>{$t('settings.crossfadeSeconds')}</span></div>
      <div class="range">
        <input
          type="range"
          min="1"
          max="12"
          step="1"
          value={$settings.crossfadeSeconds}
          disabled={!$settings.crossfade || directAudio}
          aria-label={$t('settings.crossfadeSeconds')}
          on:input={(e) => settings.set('crossfadeSeconds', Number(e.currentTarget.value))}
        />
        <span class="num value">{$t('settings.seconds', { n: $settings.crossfadeSeconds })}</span>
      </div>
    </div>
    <div class="row" class:disabled={directAudio}>
      <div class="label">
        <span>{$t('settings.normalize')}</span>
        <small>{$t('settings.normalizeHint')}</small>
      </div>
      <button class="switch" role="switch" aria-checked={$settings.normalize && !directAudio} aria-label={$t('settings.normalize')} disabled={directAudio} on:click={() => settings.set('normalize', !$settings.normalize)}>
        <span class="knob"></span>
      </button>
    </div>
    <div class="row">
      <div class="label">
        <span>{$t('settings.autoplay')}</span>
        <small>{$t('settings.autoplayHint')}</small>
      </div>
      <button class="switch" role="switch" aria-checked={$settings.autoplay} aria-label={$t('settings.autoplay')} on:click={() => settings.set('autoplay', !$settings.autoplay)}>
        <span class="knob"></span>
      </button>
    </div>
  </section>

  <section>
    <h2>{$t('settings.display')}</h2>
    <div class="row">
      <div class="label"><span>{$t('settings.trackView')}</span></div>
      <div class="segmented" role="radiogroup" aria-label={$t('settings.trackView')}>
        {#each ['compact', 'list'] as view}
          <button role="radio" aria-checked={$settings.trackView === view} class:active={$settings.trackView === view} on:click={() => settings.set('trackView', view)}>{$t(`view.${view}`)}</button>
        {/each}
      </div>
    </div>
    <div class="row">
      <div class="label">
        <span>{$t('settings.pageTint')}</span>
        <small>{$t('settings.pageTintHint')}</small>
      </div>
      <div class="segmented" role="radiogroup" aria-label={$t('settings.pageTint')}>
        {#each ['full', 'header'] as mode}
          <button role="radio" aria-checked={$settings.pageTint === mode} class:active={$settings.pageTint === mode} on:click={() => settings.set('pageTint', mode)}>{$t(`settings.pageTint.${mode}`)}</button>
        {/each}
      </div>
    </div>
    <div class="row">
      <div class="label">
        <span>{$t('settings.animatedBg')}</span>
        <small>{$t('settings.animatedBgHint')}</small>
      </div>
      <button class="switch" role="switch" aria-checked={$settings.animatedBg} aria-label={$t('settings.animatedBg')} on:click={() => settings.set('animatedBg', !$settings.animatedBg)}>
        <span class="knob"></span>
      </button>
    </div>
  </section>

  {#if lastfm?.available}
    <section>
      <h2>Last.fm</h2>
      {#if lastfm.username}
        <div class="row">
          <div class="label">
            <span>{$t('lastfm.connectedAs')} <a href="https://www.last.fm/user/{encodeURIComponent(lastfm.username)}" target="_blank" rel="noopener">{lastfm.username}</a></span>
            <small>{lastfm.pending ? $t('lastfm.pending', { n: lastfm.pending }) : $t('lastfm.hint')}</small>
          </div>
          <button class="pill outline" disabled={lastfmBusy} on:click={unlinkLastfm}>{$t('lastfm.unlink')}</button>
        </div>
        <div class="row">
          <div class="label">
            <span>{$t('lastfm.scrobble')}</span>
            <small>{$t('lastfm.scrobbleHint')}</small>
          </div>
          <button class="switch" role="switch" aria-checked={lastfm.scrobble} aria-label={$t('lastfm.scrobble')} disabled={lastfmBusy} on:click={() => setLastfm({ scrobble: !lastfm.scrobble })}>
            <span class="knob"></span>
          </button>
        </div>
        <div class="row">
          <div class="label">
            <span>{$t('lastfm.love')}</span>
            <small>{$t('lastfm.loveHint')}</small>
          </div>
          <button class="switch" role="switch" aria-checked={lastfm.love} aria-label={$t('lastfm.love')} disabled={lastfmBusy} on:click={() => setLastfm({ love: !lastfm.love })}>
            <span class="knob"></span>
          </button>
        </div>
      {:else}
        <div class="row">
          <div class="label">
            <span>{$t('lastfm.connectTitle')}</span>
            <small>{$t('lastfm.connectHint')}</small>
          </div>
          {#if lastfmBusy}
            <span class="muted"><LoaderCircle class="spin" size={18} /></span>
          {:else}
            <a class="pill outline" href={lastfm.auth_url}>{$t('lastfm.connect')}</a>
          {/if}
        </div>
      {/if}
      <p class="credit">{$t('lastfm.credit')}</p>
    </section>
  {/if}

  {#if apps}
    <section>
      <h2>{$t('apps.title')}</h2>
      {#if apps.enabled}
        {#each [['apps.server', location.origin], ['apps.username', apps.username], ['apps.password', apps.password]] as [label, value]}
          <div class="row">
            <div class="label">
              <span>{$t(label)}</span>
              <small class="value-text">{value}</small>
            </div>
            <button class="icon-btn" aria-label={$t('apps.copy')} title={$t('apps.copy')} on:click={() => copy(value)}><Copy size={18} /></button>
          </div>
        {/each}
        <p class="note">{$t('apps.passwordHint')}</p>
        <div class="apps-actions">
          <button class="pill outline" disabled={appsBusy} on:click={newAppPassword}>{$t('apps.newPassword')}</button>
          <button class="pill outline" disabled={appsBusy} on:click={disableApps}>{$t('apps.turnOff')}</button>
        </div>
      {:else}
        <div class="row">
          <div class="label">
            <span>{$t('apps.enable')}</span>
            <small>{$t('apps.hint')}</small>
          </div>
          <button class="pill outline" disabled={appsBusy} on:click={newAppPassword}>{$t('apps.turnOn')}</button>
        </div>
      {/if}
    </section>
  {/if}

  <section>
    <h2>{$t('settings.account')}</h2>
    <div class="row">
      <div class="label">
        <span>{$t('settings.loggedInAs', { name: me?.username || '' })}</span>
        <small>{$t('settings.device')}</small>
      </div>
      <button class="pill outline" on:click={signOut}><LogOut size={16} /> {$t('account.logout')}</button>
    </div>
    <div class="row">
      <div class="label">
        <span>{$t('settings.password')}</span>
        <small>{$t('settings.passwordHint')}</small>
      </div>
      {#if !pwOpen}<button class="pill outline" on:click={() => (pwOpen = true)}>{$t('settings.changePassword')}</button>{/if}
    </div>
    {#if pwOpen}
      <form class="pw-form" on:submit|preventDefault={changePassword}>
        <input type="password" bind:value={pwOld} placeholder={$t('settings.currentPassword')} autocomplete="current-password" aria-label={$t('settings.currentPassword')} />
        <input type="password" bind:value={pwNew} placeholder={$t('login.newPassword')} autocomplete="new-password" aria-label={$t('login.newPassword')} />
        <input type="password" bind:value={pwNew2} placeholder={$t('login.repeatPassword')} autocomplete="new-password" aria-label={$t('login.repeatPassword')} />
        {#if pwError}<p class="error-text">{pwError}</p>{/if}
        <div class="pw-actions">
          <button type="button" class="pill outline" on:click={() => ((pwOpen = false), (pwError = ''))}>{$t('settings.cancel')}</button>
          <button type="submit" class="pill save" disabled={pwBusy || !pwOld || !pwNew}>{$t('edit.save')}</button>
        </div>
      </form>
    {/if}
  </section>

  {#if version}<p class="version">kaikku {version}</p>{/if}
</div>

<style>
  .settings {
    padding-top: 32px;
    max-width: 840px;
  }
  h1 {
    font-size: 32px;
    margin-bottom: 24px;
  }
  section {
    margin-bottom: 32px;
  }
  .pw-form {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 360px;
    padding: 8px 0 4px;
  }
  .pw-form input {
    height: 40px;
    padding: 0 12px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #2a2a2a;
    color: var(--text);
    font: inherit;
  }
  .pw-form input:focus {
    outline: none;
    border-color: #6a6a6a;
  }
  .pw-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }
  .save {
    background: #fff;
    color: #000;
    font-weight: 700;
  }
  h2 {
    font-size: 18px;
    margin-bottom: 8px;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    padding: 14px 0;
  }
  .label a {
    color: inherit;
    font-weight: 700;
  }
  @media (hover: hover) {
    .label a:hover {
      text-decoration: underline;
    }
  }
  a.pill {
    display: inline-flex;
    align-items: center;
    text-decoration: none;
  }
  .version {
    margin: 24px 0 0;
    font-size: 13px;
    color: var(--text-dim);
    user-select: all;
  }
  .note {
    margin: -4px 0 4px;
    font-size: 13px;
    color: var(--text-dim);
  }
  .value-text {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    color: var(--text) !important;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .icon-btn {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .icon-btn:hover {
      color: var(--text);
      background: #2a2a2a;
    }
  }
  .apps-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    padding-top: 8px;
  }
  .credit {
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--text-dim);
  }
  .row.disabled {
    opacity: 0.45;
  }
  .label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .label span {
    font-size: 15px;
  }
  .label small {
    font-size: 13px;
    color: var(--text-dim);
  }
  .switch {
    flex-shrink: 0;
    width: 40px;
    height: 24px;
    border-radius: 999px;
    background: #727272;
    position: relative;
    transition: background-color 0.15s;
  }
  .switch[aria-checked='true'] {
    background: var(--player-accent, var(--accent));
  }
  .knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #000;
    transition: transform 0.15s;
  }
  .switch[aria-checked='true'] .knob {
    transform: translateX(16px);
  }
  .segmented {
    flex-shrink: 0;
    display: flex;
    gap: 4px;
    padding: 4px;
    border-radius: 999px;
    background: var(--bg-card);
  }
  .segmented button {
    padding: 6px 14px;
    border-radius: 999px;
    font-size: 14px;
    color: var(--text-dim);
  }
  .segmented button.active {
    background: var(--text);
    color: var(--bg);
  }
  .range {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }
  .range input {
    width: 180px;
    accent-color: var(--player-accent, var(--accent));
  }
  .value {
    min-width: 40px;
    text-align: right;
  }
  @media (max-width: 768px) {
    .settings {
      padding-top: 20px;
    }
    h1 {
      font-size: 26px;
    }
    .range input {
      width: 120px;
    }
  }
</style>
