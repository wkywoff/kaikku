<script>
  // Фон полноэкранного плеера: четыре мягких пятна в цвета обложки
  // (palette.mesh, color.js), которые медленно плавают, как у Apple Music.
  //
  // Рисуется шейдером WebGL, а не CSS-градиентами: Chrome сглаживает
  // CSS-градиенты клетчатым узором (соседние точки ±1 уровень яркости), и на
  // тёмном фоне эта клетка видна. Здесь каждая точка считается сама, без
  // всякого шума (даже ±1 уровень на тёмном виден как зерно), в настоящем
  // половинном разрешении: фон плавный, растяжение его не портит (пятнышки
  // от растяжения были только у шума), а видеокарте в 4–16 раз меньше работы
  // — иначе на свайпе плеера падала частота кадров.
  //
  // animated=false — пятна стоят (настройка «Живой фон плеера»). Без WebGL —
  // ровный фон цвета плеера.
  import { onMount } from 'svelte';
  import { paletteForImage, DEFAULT_PALETTE, hslToRgb } from '../color.js';

  export let cover = null;
  export let animated = true;

  let canvas;
  let failed = false;

  // Пятна: где стоят (x — доля ширины, y — доля высоты, снизу вверх),
  // радиус (доля высоты экрана), как далеко и с каким периодом плавают.
  const BLOBS = [
    { x: 0.15, y: 0.85, r: 0.55, ax: 0.22, ay: 0.18, period: 19, phase: 0 },
    { x: 0.85, y: 0.8, r: 0.5, ax: 0.2, ay: 0.22, period: 23, phase: 2.1 },
    { x: 0.25, y: 0.15, r: 0.6, ax: 0.25, ay: 0.16, period: 27, phase: 4.2 },
    { x: 0.8, y: 0.2, r: 0.5, ax: 0.18, ay: 0.2, period: 21, phase: 1.3 },
  ];
  const FADE_MS = 1200; // смена цветов при смене трека
  const FRAME_MS = 1000 / 30; // движение медленное — 30 кадров хватает

  const VERT = `attribute vec2 a; void main() { gl_Position = vec4(a, 0.0, 1.0); }`;
  const FRAG = `
    precision highp float;
    uniform vec2 uRes;
    uniform vec3 uBase;
    uniform vec3 uC[4];
    uniform vec2 uP[4];
    uniform float uR[4];
    void main() {
      vec2 p = gl_FragCoord.xy / uRes.y;
      // Цвет точки — среднее цветов пятен с весом по близости (гауссиана),
      // плюс немного цвета плеера: между пятнами не чёрные дыры.
      vec3 acc = uBase * 0.35;
      float sum = 0.35;
      for (int i = 0; i < 4; i++) {
        vec2 d = p - uP[i];
        float w = exp(-dot(d, d) / (uR[i] * uR[i]));
        acc += uC[i] * w;
        sum += w;
      }
      vec3 col = acc / sum;
      // Книзу темнее — белый текст читается на любом цвете.
      col *= mix(0.7, 0.88, gl_FragCoord.y / uRes.y);
      gl_FragColor = vec4(col, 1.0);
    }`;

  const toVec = (c) => [c.r / 255, c.g / 255, c.b / 255];
  function coloursOf(p) {
    const mesh = (p.mesh || DEFAULT_PALETTE.mesh).map((c) => toVec(hslToRgb(c)));
    return { mesh, base: toVec(hslToRgb(p.dark || DEFAULT_PALETTE.dark)) };
  }

  let from = coloursOf(DEFAULT_PALETTE);
  let to = from;
  let fadeStart = 0;
  let requestFrame = () => {};

  $: setCover(cover);
  function setCover(url) {
    paletteForImage(url).then((p) => {
      if (url !== cover) return;
      from = current(performance.now());
      to = coloursOf(p);
      fadeStart = performance.now();
      requestFrame();
    });
  }
  $: animated, requestFrame();

  const lerp = (a, b, k) => a.map((v, i) => v + (b[i] - v) * k);
  function current(now) {
    const k = fadeStart ? Math.min(1, (now - fadeStart) / FADE_MS) : 1;
    const e = k * k * (3 - 2 * k);
    return { mesh: from.mesh.map((c, i) => lerp(c, to.mesh[i], e)), base: lerp(from.base, to.base, e) };
  }

  // Компиляция шейдера синхронная и на телефоне занимает заметное время:
  // если начать её сразу, первое открытие плеера «задумывалось» на секунду.
  // Поэтому сначала открывается сам плеер (fly, 220 мс) на ровном фоне цвета
  // плеера, а живой фон запускается после и плавно проявляется (ready).
  let ready = false;
  onMount(() => {
    let stop = () => {};
    const timer = setTimeout(() => (stop = start() || stop), 260);
    return () => {
      clearTimeout(timer);
      stop();
    };
  });

  function start() {
    const gl = canvas.getContext('webgl', { antialias: false, depth: false, alpha: false, powerPreference: 'low-power' });
    if (!gl) {
      failed = true;
      return;
    }
    const shader = (type, src) => {
      const s = gl.createShader(type);
      gl.shaderSource(s, src);
      gl.compileShader(s);
      return s;
    };
    const prog = gl.createProgram();
    gl.attachShader(prog, shader(gl.VERTEX_SHADER, VERT));
    gl.attachShader(prog, shader(gl.FRAGMENT_SHADER, FRAG));
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      failed = true;
      return;
    }
    gl.useProgram(prog);
    const buf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
    const a = gl.getAttribLocation(prog, 'a');
    gl.enableVertexAttribArray(a);
    gl.vertexAttribPointer(a, 2, gl.FLOAT, false, 0, 0);
    const u = (name) => gl.getUniformLocation(prog, name);
    const uRes = u('uRes');
    const uBase = u('uBase');
    const uC = u('uC');
    const uP = u('uP');
    const uR = u('uR');
    gl.uniform1fv(uR, BLOBS.map((b) => b.r));

    const SCALE = 0.5; // точек холста на CSS-пиксель
    const resize = () => {
      const w = Math.max(1, Math.round(canvas.clientWidth * SCALE));
      const h = Math.max(1, Math.round(canvas.clientHeight * SCALE));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
        gl.viewport(0, 0, w, h);
      }
      return [w, h];
    };

    // Часы пятен идут, только пока они движутся: после паузы (настройка,
    // свайп плеера) движение продолжается с того же места, без скачка.
    let clock = 0;
    let tick = 0;
    let frame = 0;
    let last = 0;
    function draw(now) {
      frame = 0;
      const moving = animated;
      const fading = fadeStart && now - fadeStart < FADE_MS;
      if (moving && now - last < FRAME_MS) {
        frame = requestAnimationFrame(draw);
        return;
      }
      last = now;
      if (moving && tick) clock += Math.min(now - tick, 100);
      tick = moving ? now : 0;
      const [w, h] = resize();
      const t = clock / 1000;
      const aspect = w / h;
      const pos = BLOBS.flatMap((b) => {
        const k = (2 * Math.PI * t) / b.period + b.phase;
        return [(b.x + b.ax * Math.sin(k)) * aspect, b.y + b.ay * Math.cos(k * 0.8)];
      });
      const c = current(now);
      gl.uniform2f(uRes, w, h);
      gl.uniform3fv(uBase, c.base);
      gl.uniform3fv(uC, c.mesh.flat());
      gl.uniform2fv(uP, pos);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      ready = true;
      if (moving || fading) frame = requestAnimationFrame(draw);
    }
    requestFrame = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };
    const observer = new ResizeObserver(requestFrame);
    observer.observe(canvas);
    requestFrame();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      requestFrame = () => {};
      gl.getExtension('WEBGL_lose_context')?.loseContext();
    };
  }
</script>

<canvas bind:this={canvas} class:failed class:ready aria-hidden="true"></canvas>

<style>
  canvas {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    z-index: -1;
    pointer-events: none;
    display: block;
    opacity: 0;
    transition: opacity 0.5s;
  }
  canvas.ready {
    opacity: 1;
  }
  canvas.failed {
    display: none;
  }
</style>
