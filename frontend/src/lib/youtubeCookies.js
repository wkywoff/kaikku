// Куки YouTube на странице «Не нашлось» (MissingPage): что сказать после
// загрузки и что подсказать у песни, которой нужен вход в YouTube.

// Ответ PUT /api/admin/youtube/cookies ({ check, signin, requeued }) → тост:
// { key, params, kind }. Сначала то, что мешает: YouTube не пускает сервер
// или не отдаёт песни, которым нужен вход (аккаунт не подтвердил возраст).
export function cookieUploadMessage({ check, signin, requeued } = {}) {
  if (check === 'blocked') return { key: 'missing.cookiesCheck.blocked', kind: 'error' };
  if (signin === 'age') return { key: 'missing.cookiesSignIn.age', kind: 'error' };
  if (signin === 'refused') return { key: 'missing.cookiesSignIn.refused', kind: 'error' };
  if (requeued > 0) return { key: 'missing.cookiesCheck.requeued', params: { n: requeued }, kind: 'info' };
  if (check === 'error' || signin === 'error') return { key: 'missing.cookiesCheck.error', kind: 'error' };
  if (check === 'ok' || signin === 'ok') return { key: 'missing.cookiesCheck.ok', kind: 'info' };
  return { key: 'missing.cookiesCheck.skipped', kind: 'info' };
}

// Подсказка у песни вида signin (18+, для спонсоров канала): кук нет —
// загрузить; с ними YouTube хочет, чтобы аккаунт подтвердил возраст; или
// куки есть — повторить.
export function signInHint(error, haveCookies) {
  if (!haveCookies) return 'missing.signinNoCookies';
  return /provide more info|an adult/i.test(error || '') ? 'missing.signinAge' : 'missing.signinRetry';
}
