// Сервис-воркер: с ним сайт ставится как приложение и оболочка открывается
// быстро. Трогает только статику фронта:
//   страница (навигация) — из сети, чтобы после деплоя сразу была новая;
//     нет сети — последняя сохранённая;
//   /assets/* — имена с хешем и не меняются, поэтому из кеша.
// API, музыка (/media, /api/stream) и картинки идут мимо него как обычно.
//
// Файл — шаблон: при сборке vite.config.js подставляет в имя кеша id
// сборки. Каждый деплой меняет sw.js, браузер ставит новый воркер, а тот
// при активации удаляет кеш прошлой сборки — иначе старые бандлы копились
// бы на устройствах вечно.
const CACHE = 'kaikku-shell-__BUILD__';

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) => Promise.all(names.filter((n) => n !== CACHE).map((n) => caches.delete(n))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put('/', copy));
          }
          return res;
        })
        .catch(() => caches.match('/').then((hit) => hit || Response.error())),
    );
    return;
  }

  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((res) => {
            if (res.ok) {
              const copy = res.clone();
              caches.open(CACHE).then((c) => c.put(req, copy));
            }
            return res;
          }),
      ),
    );
  }
});
