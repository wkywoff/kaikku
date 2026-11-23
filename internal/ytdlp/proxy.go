package ytdlp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
)

// Прокси к YouTube (YTDLP_PROXY). YouTube не отдаёт видео на многие адреса
// дата-центров («Sign in to confirm you're not a bot»), а через другой
// выход, например Cloudflare WARP, отдаёт. Через прокси идёт всё, что
// ходит на YouTube: yt-dlp и его помощник, поиск YouTube Music и докачка
// звука по ссылке googlevideo (Fetch) — ссылка действует только с того IP,
// с которого её получили.
var proxy string

// SetProxy sends everything that talks to YouTube through the proxy at raw
// (socks5://host:port, socks5h://… or http://host:port); "" means directly.
// Call it at startup, before EnableWarm: the helper takes the proxy when it
// starts.
func SetProxy(raw string) error {
	if raw == "" {
		proxy = ""
		httpClient, musicClient = &http.Client{}, &http.Client{}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Port() == "" {
		return fmt.Errorf("want socks5://host:port or http://host:port, got %q", raw)
	}
	switch u.Scheme {
	case "socks5", "socks5h", "http", "https": // понимают и yt-dlp, и net/http
	default:
		return fmt.Errorf("unsupported proxy scheme %q: use socks5://, socks5h:// or http://", u.Scheme)
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyURL(u)
	proxy = u.String()
	httpClient, musicClient = &http.Client{Transport: t}, &http.Client{Transport: t}
	log.Printf("youtube: via proxy %s", u.Host)
	return nil
}

// command runs yt-dlp with what every call needs: the proxy and a copy of
// the cookies (cookies.go). Call done once the process has exited: it
// removes the copy.
func command(ctx context.Context, args ...string) (cmd *exec.Cmd, done func()) {
	var common []string
	if proxy != "" {
		common = append(common, "--proxy", proxy)
	}
	cookies, done := cookieCopy()
	if cookies != "" {
		common = append(common, "--cookies", cookies)
	}
	return execCommand(ctx, "yt-dlp", append(common, args...)...), done
}

// warmEnv — добавки к окружению помощника (helper.py читает их при
// старте); cookies — его копия кук.
func warmEnv(cookies string) []string {
	env := []string{warmMallocArenas}
	if proxy != "" {
		env = append(env, "KAIKKU_YTDLP_PROXY="+proxy)
	}
	if cookies != "" {
		env = append(env, "KAIKKU_YTDLP_COOKIES="+cookies)
	}
	return env
}
