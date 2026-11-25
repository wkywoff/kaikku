import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

const backendTarget = process.env.VITE_BACKEND_TARGET || 'http://localhost:8080'
const mediaTarget = process.env.VITE_MEDIA_TARGET || backendTarget

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
