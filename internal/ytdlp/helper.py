# Долгоживущий yt-dlp для API (warm.go): модуль импортирован один раз, и
# поиск/извлечение не платят за старт Python и загрузку экстракторов при
# каждом клике.
#
# Протокол — JSON по строке в stdin и stdout:
#   {"id": 1, "op": "search", "query": "ytsearch15:…"}
#       → {"id": 1, "entries": [{"id", "title", "channel", "uploader", "duration"}, …]}
#   {"id": 2, "op": "extract", "video": "<id>"}
#       → {"id": 2, "url": "…", "protocol": "https", "http_headers": {…}}
#   ошибка → {"id": N, "error": "…"}
# Запросы выполняются параллельно, ответы — в порядке готовности.
#
# Ускорение (замер на сервере, 2026-10, yt-dlp 2026.08.19):
#   - конфиг веб-клиента YouTube (ytcfg: версия клиента, visitorData) yt-dlp
#     скачивает с главной страницы перед каждым поиском — ~0.3 с; здесь он
#     живёт в памяти YTCFG_TTL. Поиск 0.75 → 0.45 с;
#   - извлечение по умолчанию грузит страницу ролика (или /next) — ~0.9 с
#     ответа YouTube ради метаданных, которые нам не нужны (они уже из
#     Deezer). Быстрый путь — штатные настройки yt-dlp player_skip =
#     webpage, configs, initial_data с visitor_data из того же конфига:
#     1.35 → 0.45 с. Без visitor_data YouTube просит подтвердить, что это не
#     бот, поэтому только вместе. Не вышло — обычное извлечение, как раньше.
import copy
import json
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor

out = sys.stdout
sys.stdout = sys.stderr  # stdout — только для протокола

import yt_dlp  # noqa: E402

YTCFG_TTL = 30 * 60


class Quiet:
    def debug(self, msg):
        pass

    info = warning = error = debug


# --- конфиг клиента в памяти ---

ytcfg_cache = {}  # client → (время, ytcfg)
ytcfg_lock = threading.Lock()


def patch_ytcfg_cache():
    try:
        from yt_dlp.extractor.youtube._base import YoutubeBaseInfoExtractor as Base
    except ImportError:
        try:
            from yt_dlp.extractor.youtube import YoutubeBaseInfoExtractor as Base
        except ImportError:
            return False
    orig = getattr(Base, '_download_ytcfg', None)
    if orig is None:
        return False

    def cached(self, client, video_id):
        if client == 'web_embedded':  # адрес зависит от ролика
            return orig(self, client, video_id)
        with ytcfg_lock:
            hit = ytcfg_cache.get(client)
        if hit and time.time() - hit[0] < YTCFG_TTL:
            return copy.deepcopy(hit[1])
        cfg = orig(self, client, video_id)
        if cfg:
            with ytcfg_lock:
                ytcfg_cache[client] = (time.time(), copy.deepcopy(cfg))
        return cfg

    Base._download_ytcfg = cached
    return True


patched = patch_ytcfg_cache()


def forget_ytcfg():
    with ytcfg_lock:
        ytcfg_cache.clear()


def visitor_data():
    """visitorData веб-клиента — из конфига в памяти (или скачать его)."""
    if not patched:
        return None
    try:
        ie = ydl('search').get_info_extractor('YoutubeSearch')
        cfg = ie._download_ytcfg('web', None) or {}
    except Exception:
        return None
    return cfg.get('VISITOR_DATA') or ((cfg.get('INNERTUBE_CONTEXT') or {}).get('client') or {}).get('visitorData')


# --- YoutubeDL на поток ---

local = threading.local()


def options(kind, visitor=None):
    opts = {'logger': Quiet(), 'quiet': True, 'no_warnings': True,
            'skip_download': True, 'cachedir': False}
    if kind == 'search':  # как `yt-dlp -j --flat-playlist --ignore-errors`
        opts.update(extract_flat='in_playlist', ignoreerrors=True)
    else:  # как `yt-dlp -j -f 140`
        opts.update(format='140')
    if visitor:
        opts['extractor_args'] = {'youtube': {
            'player_skip': ['webpage', 'configs', 'initial_data'],
            'visitor_data': [visitor],
        }}
    return opts


def ydl(kind, visitor=None):
    # Свой YoutubeDL на поток: объект не потокобезопасен, а повторное
    # использование сохраняет загруженный JS плеера между запросами.
    key = kind if visitor is None else kind + ':fast'
    d = getattr(local, key, None)
    if d is None or (visitor is not None and local.__dict__.get(key + ':visitor') != visitor):
        d = yt_dlp.YoutubeDL(options(kind, visitor))
        setattr(local, key, d)
        if visitor is not None:
            local.__dict__[key + ':visitor'] = visitor
    return d


def extract(video):
    url = 'https://www.youtube.com/watch?v=' + video
    visitor = visitor_data()
    if visitor:
        try:
            i = ydl('extract', visitor).extract_info(url, download=False)
            if i.get('url'):
                return i
        except Exception as e:
            if 'bot' in str(e):
                forget_ytcfg()  # сессия стала подозрительной — новый конфиг в следующий раз
            print('fast extract %s: %s' % (video, e), file=sys.stderr)
    return ydl('extract').extract_info(url, download=False)


lock = threading.Lock()


def reply(msg):
    with lock:
        out.write(json.dumps(msg) + '\n')
        out.flush()


def handle(req):
    try:
        if req['op'] == 'search':
            r = ydl('search').extract_info(req['query'], download=False) or {}
            entries = [{k: e.get(k) for k in ('id', 'title', 'channel', 'uploader', 'duration')}
                       for e in (r.get('entries') or []) if e]
            reply({'id': req['id'], 'entries': entries})
        elif req['op'] == 'extract':
            i = extract(req['video'])
            reply({'id': req['id'], 'url': i.get('url'), 'protocol': i.get('protocol'),
                   'http_headers': i.get('http_headers')})
        else:
            reply({'id': req['id'], 'error': 'unknown op ' + repr(req['op'])})
    except Exception as e:  # ошибка запроса (ролик недоступен и т.п.), не процесса
        reply({'id': req['id'], 'error': str(e)})


pool = ThreadPoolExecutor(max_workers=4)
for line in sys.stdin:
    if line.strip():
        pool.submit(handle, json.loads(line))
