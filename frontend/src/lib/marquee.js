// Бегущая строка для текста, который не влезает в одну строку (название в
// полноэкранном плеере), как в Apple Music и Spotify: текст стоит, потом
// плавно уезжает влево до конца, замирает и возвращается. Влезает — стоит
// на месте. Узел — блок в одну строку, внутри — первый элемент, который
// двигается. Края затухают (класс marquee-on, стили — у компонента): правый
// — пока есть продолжение, левый (--marquee-fade, app.css) — только в
// движении, чтобы в покое первая буква была целой. Доезжает строка так,
// что последняя буква не попадает в затухание справа.
//
//   <a class="title" use:marquee={title}><span>{title}</span></a>

const PAUSE = 2000; // мс на каждом конце
const SPEED = 40; // px/с
const FADE_RIGHT = 16; // px — как в стилях компонента
const FADE_LEFT = 12; // px
const RAMP = 250; // мс — левое затухание появляется и уходит

export function marquee(node) {
  let anims = [];
  let frame = 0;

  function update() {
    frame = 0;
    const inner = node.firstElementChild;
    anims.forEach((a) => a.cancel());
    anims = [];
    if (!inner) return;
    const overflow = inner.scrollWidth - node.clientWidth;
    const on = overflow > 1;
    node.classList.toggle('marquee-on', on);
    if (!on || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const shift = overflow + FADE_RIGHT;
    const move = (shift / SPEED) * 1000;
    const total = 2 * (PAUSE + move);
    const t1 = PAUSE / total;
    const t2 = (PAUSE + move) / total;
    const t3 = (2 * PAUSE + move) / total;
    const timing = { duration: total, iterations: Infinity };
    const at = (x) => ({ transform: `translateX(${x}px)` });
    anims.push(
      inner.animate(
        [
          { ...at(0), offset: 0 },
          { ...at(0), offset: t1, easing: 'ease-in-out' },
          { ...at(-shift), offset: t2 },
          { ...at(-shift), offset: t3, easing: 'ease-in-out' },
          { ...at(0), offset: 1 },
        ],
        timing,
      ),
    );
    const fade = (x) => ({ '--marquee-fade': `${x}px` });
    const ramp = RAMP / total;
    anims.push(
      node.animate(
        [
          { ...fade(0), offset: 0 },
          { ...fade(0), offset: t1 },
          { ...fade(FADE_LEFT), offset: t1 + ramp },
          { ...fade(FADE_LEFT), offset: 1 - ramp },
          { ...fade(0), offset: 1 },
        ],
        timing,
      ),
    );
  }

  // Ширина меняется (окно, текст открыли/закрыли) — пересчитать раз за кадр.
  const schedule = () => frame || (frame = requestAnimationFrame(update));
  const ro = new ResizeObserver(schedule);
  ro.observe(node);
  schedule();

  return {
    update: schedule, // параметр — сам текст: сменился трек — заново
    destroy() {
      ro.disconnect();
      cancelAnimationFrame(frame);
      anims.forEach((a) => a.cancel());
    },
  };
}
