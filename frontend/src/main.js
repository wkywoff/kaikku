import { mount } from 'svelte'
import { get } from 'svelte/store'
import '@fontsource-variable/onest'
// Unbounded — логотип и заголовки (h1, h2: app.css, --font-display); браузер
// качает только нужные наборы символов (unicode-range).
import '@fontsource/unbounded/latin-700.css'
import '@fontsource/unbounded/cyrillic-700.css'
import '@fontsource/unbounded/latin-800.css'
import '@fontsource/unbounded/cyrillic-800.css'
import './app.css'
import App from './App.svelte'
import './lib/stores/install.js' // ловим beforeinstallprompt с самого начала
import { isPlaying } from './lib/stores/player.js'
import { updateReady } from './lib/stores/network.js'

// Ленивая часть сборки (страница, окно) не загрузилась — вкладка открыта
// до обновления, а её файлов уже нет (в образе web — только текущая сборка).
// Пока ничего не играет — перезагрузить страницу, но не чаще раза в минуту
// (вдруг сломана сама новая сборка); играет — плашка «Обновить», музыку не
// обрывать.
window.addEventListener('vite:preloadError', () => {
  const KEY = 'kaikku_reloaded_at'
  let last = 0
  try {
    last = Number(sessionStorage.getItem(KEY)) || 0
  } catch {
    /* хранилище недоступно — считаем, что не перезагружали */
  }
  if (!get(isPlaying) && Date.now() - last > 60000) {
    try {
      sessionStorage.setItem(KEY, String(Date.now()))
    } catch {
      /* не запомнится — не страшно */
    }
    location.reload()
  } else {
    updateReady.set(true)
  }
})

// Сервис-воркер (src/sw.js, при сборке — /sw.js) — для установки как приложения; в dev не нужен.
// Новый воркер (каждый деплой) берёт страницу на себя сразу (sw.js) — это и
// есть «вышла новая версия»: плашка предлагает перезагрузить. При первой
// установке воркера страница тоже его получает, но это не обновление.
// Приложение с экрана iPhone само проверяет обновления редко — проверяем,
// когда его открывают снова.
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
