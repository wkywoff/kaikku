<script>
  import { api } from '../api.js';
  import { auth } from '../stores/auth.js';
  import { t } from '../i18n.js';

  // Ссылка-приглашение вида /?invite=<код> сразу открывает регистрацию,
  // ссылка /?reset=<код> (её даёт админ) — форму нового пароля.
  const params = new URLSearchParams(location.search);
  const inviteFromUrl = params.get('invite') || '';
  const resetFromUrl = params.get('reset') || '';

  let mode = resetFromUrl ? 'reset' : inviteFromUrl ? 'register' : 'login'; // 'login' | 'register' | 'reset'
  let username = '';
  let password = '';
  let password2 = '';
  let invite = inviteFromUrl;
  let busy = false;
  let error = '';
  // Пустая база: первый зарегистрированный — админ, приглашение не нужно.
  let firstUser = false;
  if (!resetFromUrl) {
    api
      .registerInfo()
      .then((r) => {
        firstUser = r.first_user;
        if (firstUser) mode = 'register';
      })
      .catch(() => {});
  }

  // Те же правила, что на сервере (internal/api/handlers_auth.go): имя —
  // 3–32 буквы, цифры, «_», «-», «.»; пароль — 8–256 символов.
  const USERNAME = /^[\p{L}\p{Nd}_.-]{3,32}$/u;

  // Пауза после неудачных входов растёт до 15 минут (internal/api/loginguard.go);
  // сколько ждать, говорит сервер.
  const waitText = (s) => (s < 60 ? $t('login.seconds', { n: s }) : $t('login.minutes', { n: Math.ceil(s / 60) }));

  function errorText(e) {
    if (e.status === 429 && e.retryAfter > 0) return $t('login.tooMany', { wait: waitText(e.retryAfter) });
    const key = { 400: 'login.invalid', 401: 'login.badCredentials', 403: mode === 'reset' ? 'login.badReset' : 'login.badInvite', 409: 'login.taken', 429: 'login.tooManyLater', 503: 'login.busy' }[e.status];
    return key ? $t(key) : e.message;
  }

  async function submit() {
    error = '';
    if (mode === 'reset') return submitReset();
    const invalid = mode === 'register' ? !USERNAME.test(username.trim()) || password.length < 8 || password.length > 256 : !username.trim() || !password;
    if (invalid) {
      error = $t('login.invalid');
      return;
    }
    busy = true;
    try {
      const { token } =
        mode === 'login' ? await api.login(username, password) : await api.register(username, password, invite.trim());
      if (inviteFromUrl) history.replaceState(null, '', location.pathname); // код одноразовый — убрать из адреса
      auth.login(token);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function submitReset() {
    if (password.length < 8 || password.length > 256) {
      error = $t('login.invalid');
      return;
    }
    if (password !== password2) {
      error = $t('login.mismatch');
      return;
    }
    busy = true;
    try {
      const { token } = await api.resetPassword(resetFromUrl, password);
      history.replaceState(null, '', location.pathname); // ссылка одноразовая — убрать из адреса
      auth.login(token);
    } catch (e) {
      error = errorText(e);
    } finally {
      busy = false;
    }
  }

  function toggleMode() {
    mode = mode === 'login' ? 'register' : 'login';
    error = '';
  }
</script>

<div class="wrap">
  <form on:submit|preventDefault={submit}>
    <svg class="logo" viewBox="0 0 100 100" width="56" height="56" aria-hidden="true">
      <path d="M30 0 H70 C88 0 100 12 100 30 V70 C100 88 88 100 70 100 H30 C12 100 0 88 0 70 V30 C0 12 12 0 30 0 Z" fill="#FF4D2E" />
      <path d="M30 22 V78 M34 50 C45.3 51.9 51.9 47.2 54 36 C56.1 24.8 62.7 20.1 74 22 M34 50 C45.3 48.1 51.9 52.8 54 64 C56.1 75.2 62.7 79.9 74 78" fill="none" stroke="#141414" stroke-width="11" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
    <h1>{mode === 'login' ? $t('login.title') : mode === 'reset' ? $t('login.resetTitle') : $t('login.registerTitle')}</h1>

    {#if mode === 'reset'}
      <label>
        {$t('login.newPassword')}
        <input type="password" bind:value={password} autocomplete="new-password" />
      </label>
      <label>
        {$t('login.repeatPassword')}
        <input type="password" bind:value={password2} autocomplete="new-password" />
      </label>
    {:else}
      <label>
        {$t('login.username')}
        <input type="text" bind:value={username} autocomplete="username" />
      </label>
      <label>
        {$t('login.password')}
        <input type="password" bind:value={password} autocomplete={mode === 'login' ? 'current-password' : 'new-password'} />
      </label>
    {/if}

    {#if mode === 'register' && firstUser}
      <p class="note">{$t('login.firstUser')}</p>
    {:else if mode === 'register'}
      <label>
        {$t('login.invite')}
        <input type="text" bind:value={invite} autocomplete="off" spellcheck="false" />
      </label>
    {/if}

    {#if error}<p class="error-text">{error}</p>{/if}

    <button class="submit" type="submit" disabled={busy}>
      {busy ? $t('login.wait') : mode === 'login' ? $t('login.submit') : mode === 'reset' ? $t('login.saveReset') : $t('login.register')}
    </button>
    {#if mode !== 'reset'}
      <button class="switch" type="button" on:click={toggleMode}>
        {mode === 'login' ? $t('login.toRegister') : $t('login.toLogin')}
      </button>
    {/if}
  </form>
</div>

<style>
  .wrap {
    min-height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: linear-gradient(180deg, #2a2a2a 0%, #000 100%);
  }
  form {
    width: 100%;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 16px;
    padding: 40px 36px;
    border-radius: 12px;
    background: #121212;
  }
  .logo {
    align-self: center;
  }
  h1 {
    text-align: center;
    font-size: 28px;
    margin-bottom: 12px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-size: 14px;
    font-weight: 600;
  }
  input {
    height: 48px;
    padding: 0 14px;
    border-radius: 4px;
    border: 1px solid #727272;
    background: #121212;
    font-size: 16px;
    transition: border-color 0.15s;
  }
  @media (hover: hover) {
    input:hover {
      border-color: #fff;
    }
  }
  input:focus {
    outline: none;
    border-color: #fff;
    box-shadow: 0 0 0 1px #fff;
  }
  .submit {
    height: 48px;
    margin-top: 8px;
    border-radius: 999px;
    background: #fff;
    color: #000;
    font-weight: 700;
    font-size: 16px;
    transition: transform 0.1s;
  }
  @media (hover: hover) {
    .submit:hover:not(:disabled) {
      transform: scale(1.02);
    }
  }
  .note {
    margin: 0;
    color: var(--text-dim);
    font-size: 14px;
    line-height: 1.4;
  }
  .switch {
    color: var(--text-dim);
    font-size: 14px;
    padding: 4px;
  }
  @media (hover: hover) {
    .switch:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  @media (max-width: 480px) {
    .wrap {
      padding: 0;
      background: #121212;
    }
    form {
      padding: 32px 20px;
    }
  }
</style>
