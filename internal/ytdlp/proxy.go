package ytdlp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
)

var proxy string

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
	case "socks5", "socks5h", "http", "https":
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

func command(ctx context.Context, signIn bool, args ...string) (cmd *exec.Cmd, done func()) {
	var common []string
	if proxy != "" {
		common = append(common, "--proxy", proxy)
	}
	done = func() {}
	if signIn {
		var cookies string
		if cookies, done = cookieCopy(); cookies != "" {
			common = append(common, "--cookies", cookies)
		}
	}
	return execCommand(ctx, "yt-dlp", append(common, args...)...), done
}

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
