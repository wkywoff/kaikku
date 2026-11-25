import { mount } from 'svelte'
import { get } from 'svelte/store'
import '@fontsource-variable/onest'
import '@fontsource/unbounded/latin-700.css'
import '@fontsource/unbounded/cyrillic-700.css'
import '@fontsource/unbounded/latin-800.css'
import '@fontsource/unbounded/cyrillic-800.css'
import './app.css'
import App from './App.svelte'
import './lib/stores/install.js'
import { isPlaying } from './lib/stores/player.js'
import { updateReady } from './lib/stores/network.js'

window.addEventListener('vite:preloadError', () => {
  const KEY = 'kaikku_reloaded_at'
  let last = 0
  try {
    last = Number(sessionStorage.getItem(KEY)) || 0
  } catch {
  }
  if (!get(isPlaying) && Date.now() - last > 60000) {
    try {
      sessionStorage.setItem(KEY, String(Date.now()))
    } catch {
    }
    location.reload()
  } else {
    updateReady.set(true)
  }
})

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  const hadWorker = !!navigator.serviceWorker.controller
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (hadWorker) updateReady.set(true)
  })
  navigator.serviceWorker
    .register('/sw.js')
    .then((reg) => {
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') reg.update().catch(() => {})
      })
    })
    .catch(() => {})
}

const app = mount(App, {
  target: document.getElementById('app'),
})

export default app
