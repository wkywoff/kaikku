import { get } from 'svelte/store';
import { t } from './i18n.js';
import { player } from './stores/player.js';
import { toast } from './stores/toast.js';

// Таймер сна (кнопка-луна в плеере): варианты как в Spotify.
const MINUTES = [5, 10, 15, 30, 45, 60];

/** Сколько минут осталось (округление вверх), для подписи. */
export function sleepMinutesLeft(sleep, now = Date.now()) {
  return sleep?.at ? Math.max(1, Math.ceil((sleep.at - now) / 60000)) : 0;
}

export function sleepLabel(sleep, tr = get(t)) {
  if (!sleep) return tr('sleep.title');
  return sleep.endOfTrack ? tr('sleep.endOfTrackOn') : tr('sleep.left', { n: sleepMinutesLeft(sleep) });
}

export function sleepMenuItems() {
  const tr = get(t);
  const sleep = get(player).sleep;
  const set = (value, text) => () => {
    player.setSleepTimer(value);
    toast(text);
  };
  return [
    { heading: sleepLabel(sleep, tr) },
    ...MINUTES.map((n) => ({
      label: n === 60 ? tr('sleep.hour') : tr('sleep.minutes', { n }),
      onClick: set(n, tr('sleep.set', { n })),
    })),
    { label: tr('sleep.endOfTrack'), checked: !!sleep?.endOfTrack, onClick: set('track', tr('sleep.endOfTrackOn')) },
    ...(sleep ? [{ separator: true }, { label: tr('sleep.off'), onClick: set(null, tr('sleep.offDone')) }] : []),
  ];
}
