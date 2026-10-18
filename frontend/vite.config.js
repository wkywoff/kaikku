import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

// Дев-прокси на бэкенд: /api и /media (обложки + защищённые треки) идут на
// сам Go-процесс/nginx как в проде, чтобы не городить CORS для локальной
// разработки. VITE_BACKEND_TARGET переопределяем при необходимости.
const backendTarget = process.env.VITE_BACKEND_TARGET || 'http://localhost:8080'
// /media в проде отдаёт nginx, не Go; для локального стенда можно указать
// отдельный сервер файлов.
const mediaTarget = process.env.VITE_MEDIA_TARGET || backendTarget

// Сервис-воркер (src/sw.js) кладётся в корень сборки как /sw.js, с id
// сборки в имени кеша (хеш имён всех файлов бандла — они сами с хешами).
function serviceWorker() {
  return {
    name: 'kaikku-sw',
    apply: 'build',
    generateBundle(_, bundle) {
      const build = createHash('sha256').update(Object.keys(bundle).sort().join('\n')).digest('hex').slice(0, 12)
      const source = readFileSync(new URL('./src/sw.js', import.meta.url), 'utf8').replace('__BUILD__', build)
      this.emitFile({ type: 'asset', fileName: 'sw.js', source })
    },
  }
}

// Шрифты, которые нужны сразу: текст интерфейса (Onest, кириллица и
// латиница) и заголовки (Unbounded 700, кириллица). Без предзагрузки браузер
// узнаёт о них, только когда JS нарисует текст, — и показывает его сначала
// запасным шрифтом, а потом перерисовывает. Остальные наборы символов
// грузятся по unicode-range, когда встретятся.
const PRELOAD_FONTS = [/onest-cyrillic-wght-normal-.*\.woff2$/, /onest-latin-wght-normal-.*\.woff2$/, /unbounded-cyrillic-700-normal-.*\.woff2$/];

function preloadFonts() {
  return {
    name: 'kaikku-preload-fonts',
    apply: 'build',
    transformIndexHtml: {
      order: 'post',
      handler(_, ctx) {
        return Object.keys(ctx.bundle || {})
          .filter((file) => PRELOAD_FONTS.some((re) => re.test(file)))
          .map((file) => ({
            tag: 'link',
            attrs: { rel: 'preload', href: `/${file}`, as: 'font', type: 'font/woff2', crossorigin: '' },
            injectTo: 'head',
          }));
      },
    },
  };
}

export default defineConfig({
  plugins: [svelte(), serviceWorker(), preloadFonts()],
  server: {
    proxy: {
      '/api': { target: backendTarget, changeOrigin: true },
      '/media': { target: mediaTarget, changeOrigin: true },
    },
  },
})
