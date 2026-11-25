import copy
import json
import os
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor

out = sys.stdout
sys.stdout = sys.stderr

import yt_dlp  # noqa: E402

YTCFG_TTL = 30 * 60
QUICK_CLIENTS = ['visionos']
PROXY = os.environ.get('KAIKKU_YTDLP_PROXY')
COOKIES = os.environ.get('KAIKKU_YTDLP_COOKIES')
COOKIES_FIRST_FOR = 30 * 60

SIGN_IN = ('--cookies', 'confirm your age', 'age-restricted', 'inappropriate for some users',
           'members-only', 'join this channel', 'granted access')
REFUSED = ('not a bot', 'rate-limited by youtube')
cookies_first_until = 0.0


def refused(e):
    s = str(e).lower()
    return any(m in s for m in REFUSED)


def wants_sign_in(e):
    s = str(e).lower()
    return refused(e) or any(m in s for m in SIGN_IN)


class Quiet:
    def debug(self, msg):
        pass

    info = warning = debug

    def error(self, msg):
        local.last_error = msg


ytcfg_cache = {}
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
        if client == 'web_embedded':
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
    if not patched:
        return None
    try:
        ie = ydl('search').get_info_extractor('YoutubeSearch')
        cfg = ie._download_ytcfg('web', None) or {}
    except Exception:
        return None
    return cfg.get('VISITOR_DATA') or ((cfg.get('INNERTUBE_CONTEXT') or {}).get('client') or {}).get('visitorData')


local = threading.local()


def options(kind, visitor=None, signed_in=False):
    opts = {'logger': Quiet(), 'quiet': True, 'no_warnings': True,
            'skip_download': True, 'cachedir': False}
    if PROXY:
        opts['proxy'] = PROXY
    if signed_in:
        opts['cookiefile'] = COOKIES
    if kind == 'search':
        opts.update(extract_flat='in_playlist', ignoreerrors=True)
    else:
        opts.update(format='140')
    args = {}
    if kind == 'quick':
        args['player_client'] = QUICK_CLIENTS
    if visitor:
        args.update(player_skip=['webpage', 'configs', 'initial_data'],
                    visitor_data=[visitor])
    if args:
        opts['extractor_args'] = {'youtube': args}
    return opts


def ydl(kind, visitor=None, signed_in=False):
    key = kind + (':fast' if visitor is not None else '') + (':signed-in' if signed_in else '')
    d = getattr(local, key, None)
    if d is None or (visitor is not None and local.__dict__.get(key + ':visitor') != visitor):
        d = yt_dlp.YoutubeDL(options(kind, visitor, signed_in))
        setattr(local, key, d)
        if visitor is not None:
            local.__dict__[key + ':visitor'] = visitor
    return d


def extract(video):
    url = 'https://www.youtube.com/watch?v=' + video
    if COOKIES and time.time() < cookies_first_until:
        return ydl('extract', signed_in=True).extract_info(url, download=False)
    visitor = visitor_data()
    if visitor:
        try:
            i = ydl('quick', visitor).extract_info(url, download=False)
            if i.get('url'):
                return i
        except Exception as e:
            if 'bot' in str(e):
                forget_ytcfg()
            print('fast extract %s: %s' % (video, e), file=sys.stderr)
    try:
        i = ydl('quick').extract_info(url, download=False)
        if i.get('url'):
            return i
    except Exception as e:
        print('quick extract %s: %s' % (video, e), file=sys.stderr)
        if COOKIES and wants_sign_in(e):
            return extract_signed_in(video, url, e)
    try:
        return ydl('extract').extract_info(url, download=False)
    except Exception as e:
        if COOKIES and wants_sign_in(e):
            return extract_signed_in(video, url, e)
        raise


def extract_signed_in(video, url, why):
    global cookies_first_until
    if refused(why):
        cookies_first_until = time.time() + COOKIES_FIRST_FOR
    print('extract %s: signing in with the cookies' % video, file=sys.stderr)
    return ydl('extract', signed_in=True).extract_info(url, download=False)


def search(query, signed_in=False):
    local.last_error = None
    r = ydl('search', signed_in=signed_in).extract_info(query, download=False) or {}
    err = local.last_error
    if not r.get('entries') and err and wants_sign_in(err):
        if COOKIES and not signed_in:
            return search(query, signed_in=True)
        raise Exception(err)
    return r


lock = threading.Lock()


def reply(msg):
    with lock:
        out.write(json.dumps(msg) + '\n')
        out.flush()


def handle(req):
    try:
        if req['op'] == 'search':
            r = search(req['query'])
            entries = [{k: e.get(k) for k in ('id', 'title', 'channel', 'uploader', 'duration')}
                       for e in (r.get('entries') or []) if e]
            reply({'id': req['id'], 'entries': entries})
        elif req['op'] == 'extract':
            i = extract(req['video'])
            reply({'id': req['id'], 'url': i.get('url'), 'protocol': i.get('protocol'),
                   'http_headers': i.get('http_headers')})
        else:
            reply({'id': req['id'], 'error': 'unknown op ' + repr(req['op'])})
    except Exception as e:
        reply({'id': req['id'], 'error': str(e)})


pool = ThreadPoolExecutor(max_workers=4)
for line in sys.stdin:
    if line.strip():
        pool.submit(handle, json.loads(line))
