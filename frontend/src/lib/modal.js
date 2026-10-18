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
//
// «Назад». Жест «назад» на телефоне закрывает окно, а не уводит со страницы
// (backCloses, stores/nav.js). Параметр — функция закрытия окна.
//
// Фокус. Tab ходит по кругу внутри окна, после закрытия фокус возвращается
// туда, где был (focusTrap.js).
//
//   <div class="backdrop" use:modal={() => dispatch('close')}>…</div>
import { backCloses } from './stores/nav.js';
import { focusTrap } from './focusTrap.js';

let open = 0;

export function modal(node, close) {
  let onBack = close;
  if (open++ === 0) document.body.classList.add('modal-open');
  const closed = backCloses(() => onBack?.());
  const trap = focusTrap(node);
  return {
    update(next) {
      onBack = next;
    },
    destroy() {
      trap.destroy();
      closed();
      if (--open === 0) document.body.classList.remove('modal-open');
    },
  };
}
