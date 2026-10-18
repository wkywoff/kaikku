import { get } from 'svelte/store';
import { t } from './i18n.js';
import { toast } from './stores/toast.js';
import Link from '@lucide/svelte/icons/link';
import Share2 from '@lucide/svelte/icons/share-2';

// «Поделиться»: на телефоне — системное меню (мессенджеры и т.п.), на
// компьютере — ссылка в буфер обмена. path — адрес страницы в приложении.

const touch = () => typeof matchMedia === 'function' && matchMedia('(hover: none)').matches;

export async function share(path, title = '') {
  const url = location.origin + path;
  if (touch() && navigator.share) {
    try {
      await navigator.share({ url, title });
      return;
    } catch (e) {
      if (e?.name === 'AbortError') return; // закрыли меню — ничего не делаем
    }
  }
  await copyLink(path);
}

/** Ссылка на страницу в буфер обмена. */
export async function copyLink(path) {
  const url = location.origin + path;
  try {
    await navigator.clipboard.writeText(url);
    toast(get(t)('share.copied'));
  } catch {
    toast(url); // буфер недоступен — хотя бы показать ссылку
  }
}

/** «Скопировать ссылку» — всегда в буфер; на телефоне рядом «Поделиться». */
export function linkItems(path, title = '') {
  const tr = get(t);
  const items = [{ label: tr('share.copyLink'), icon: Link, onClick: () => copyLink(path) }];
  if (touch() && typeof navigator !== 'undefined' && navigator.share) {
    items.push({ label: tr('share.share'), icon: Share2, onClick: () => share(path, title) });
  }
  return items;
}

/** Пункт контекстного меню «Поделиться» / «Скопировать ссылку». */
export function shareItem(path, title = '') {
  const tr = get(t);
  return touch() && typeof navigator !== 'undefined' && navigator.share
    ? { label: tr('share.share'), icon: Share2, onClick: () => share(path, title) }
    : { label: tr('share.copyLink'), icon: Link, onClick: () => share(path, title) };
}
