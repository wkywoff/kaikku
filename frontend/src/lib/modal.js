// Окно поверх всего (правка плейлиста, правка админа, импорт, обложка во
// весь экран).
//
// Слой. Окна живут внутри страницы, а страница (<main>, App.svelte) — свой
// слой (isolation: isolate): какой бы ни был z-index у окна, оно оставалось
// под мини-плеером и нижней панелью, и на телефоне кнопки внизу окна уходили
// под плеер. Пока открыто хоть одно окно, у <body> класс modal-open, и
// страница поднимается над плеером (App.svelte). Переносить само окно в
// <body> нельзя: Svelte убирает блок, проходя по соседним узлам на старом
// месте, и окно внутри {#await} оставалось на экране после закрытия.
// Обложка во весь экран (keepPlayer) на телефоне остаётся под мини-плеером
// и нижней панелью: музыкой можно управлять, не закрывая обложку. Класс у
// <body> тогда другой — modal-keep-player.
//
// «Назад». Жест «назад» на телефоне закрывает окно, а не уводит со страницы
// (backCloses, stores/nav.js). Параметр — функция закрытия окна.
//
// Фокус. Tab ходит по кругу внутри окна, после закрытия фокус возвращается
// туда, где был (focusTrap.js).
//
//   <div class="backdrop" use:modal={() => dispatch('close')}>…</div>
//   <div class="backdrop" use:modal={{ close, keepPlayer: true }}>…</div>
import { backCloses } from './stores/nav.js';
import { focusTrap } from './focusTrap.js';

const open = {}; // класс у <body> → сколько окон его держат

const options = (arg) => (typeof arg === 'function' ? { close: arg } : arg || {});

export function modal(node, arg) {
  let { close: onBack, keepPlayer } = options(arg);
  const cls = keepPlayer ? 'modal-keep-player' : 'modal-open';
  open[cls] = (open[cls] || 0) + 1;
  document.body.classList.add(cls);
  const closed = backCloses(() => onBack?.());
  const trap = focusTrap(node);
  return {
    update(next) {
      onBack = options(next).close;
    },
    destroy() {
      trap.destroy();
      closed();
      if (--open[cls] === 0) document.body.classList.remove(cls);
    },
  };
}
