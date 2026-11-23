package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Куки YouTube (cookies.txt в формате Netscape), которые админ загружает на
// странице «Не нашлось»: с ними YouTube пускает и сервер, которому иначе
// отвечает «Sign in to confirm you're not a bot». Лежат одним файлом рядом
// со staging — на томе, общем у API и воркера (/data/youtube-cookies.txt),
// и только для youtube.com: куки других сайтов серверу не нужны.
//
// yt-dlp с --cookies при выходе записывает свою банку обратно в файл, а
// запусков бывает несколько сразу (воркер, API, помощник): общий файл они
// перетирали бы друг другу. Поэтому каждый запуск получает свою копию
// (cookieCopy), а сам файл меняет только админ.
var cookiesFile string

// SetCookiesFile sets where uploaded cookies are kept; "" turns them off.
// Call it at startup, like SetProxy.
func SetCookiesFile(path string) { cookiesFile = path }

// CookiesPath is the cookies file next to stagingDir, on the volume the API
// and the worker share.
func CookiesPath(stagingDir string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(stagingDir)), "youtube-cookies.txt")
}

// CookiesInfo describes uploaded cookies without their values.
type CookiesInfo struct {
	UpdatedAt time.Time  `json:"updated_at"`
	Count     int        `json:"count"`             // кук youtube.com
	SignedIn  bool       `json:"signed_in"`         // среди них вход в аккаунт
	Expires   *time.Time `json:"expires,omitempty"` // когда истекает вход: самая ранняя из кук входа
}

var (
	ErrCookiesJSON      = errors.New("the cookies are JSON, not a cookies.txt file (Netscape format)")
	ErrCookiesFormat    = errors.New("not a cookies.txt file (Netscape format)")
	ErrNoYouTubeCookies = errors.New("no youtube.com cookies in the file")
	ErrCookiesOff       = errors.New("youtube cookies are off on this server")
)

// Куки входа в аккаунт Google: без них YouTube просит войти так же.
var signInCookies = map[string]bool{
	"SID": true, "__Secure-1PSID": true, "__Secure-3PSID": true,
	"SAPISID": true, "__Secure-3PAPISID": true, "LOGIN_INFO": true,
}

// parseCookies keeps the youtube.com cookies of a cookies.txt file, as a
// file of their own.
func parseCookies(data []byte) ([]byte, CookiesInfo, error) {
	var info CookiesInfo
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if t := bytes.TrimSpace(data); len(t) > 0 && (t[0] == '[' || t[0] == '{') {
		return nil, info, ErrCookiesJSON // так выгружают некоторые расширения
	}
	var out bytes.Buffer
	out.WriteString("# Netscape HTTP Cookie File\n")
	entries := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		entry, httpOnly := strings.CutPrefix(line, "#HttpOnly_")
		if strings.TrimSpace(entry) == "" || !httpOnly && strings.HasPrefix(entry, "#") {
			continue
		}
		// domain, для поддоменов, path, только https, истекает, имя, значение
		f := strings.Split(entry, "\t")
		if len(f) != 7 {
			continue
		}
		expires, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil && f[4] != "" {
			continue
		}
		entries++
		domain := strings.ToLower(f[0])
		if domain != "youtube.com" && !strings.HasSuffix(domain, ".youtube.com") {
			continue
		}
		out.WriteString(line + "\n")
		info.Count++
		if signInCookies[f[5]] {
			info.SignedIn = true
			if t := time.Unix(expires, 0); expires > 0 && (info.Expires == nil || t.Before(*info.Expires)) {
				info.Expires = &t
			}
		}
	}
	switch {
	case entries == 0:
		return nil, info, ErrCookiesFormat
	case info.Count == 0:
		return nil, info, ErrNoYouTubeCookies
	}
	return out.Bytes(), info, nil
}

// SaveCookies keeps the youtube.com cookies of a cookies.txt file for every
// later yt-dlp run; the helper restarts with them.
func SaveCookies(data []byte) (CookiesInfo, error) {
	if cookiesFile == "" {
		return CookiesInfo{}, ErrCookiesOff
	}
	clean, info, err := parseCookies(data)
	if err != nil {
		return info, err
	}
	// Целиком или никак: запуск, который читает файл прямо сейчас, не должен
	// увидеть половину.
	tmp, err := os.CreateTemp(filepath.Dir(cookiesFile), ".youtube-cookies-*")
	if err != nil {
		return info, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(clean)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), cookiesFile)
	}
	if err != nil {
		return info, err
	}
	info.UpdatedAt = time.Now()
	log.Printf("youtube: cookies uploaded (%d, signed in: %v)", info.Count, info.SignedIn)
	return info, nil
}

// RemoveCookies deletes the uploaded cookies.
func RemoveCookies() error {
	if cookiesFile == "" {
		return nil
	}
	if err := os.Remove(cookiesFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	log.Printf("youtube: cookies removed")
	return nil
}

// Cookies describes the uploaded cookies; false: there are none.
func Cookies() (CookiesInfo, bool) {
	if cookiesFile == "" {
		return CookiesInfo{}, false
	}
	data, err := os.ReadFile(cookiesFile)
	if err != nil {
		return CookiesInfo{}, false
	}
	_, info, err := parseCookies(data)
	if err != nil {
		return CookiesInfo{}, false
	}
	if st, err := os.Stat(cookiesFile); err == nil {
		info.UpdatedAt = st.ModTime()
	}
	return info, true
}

// cookiesVersion changes whenever the cookies file does ("" — no file).
func cookiesVersion() string {
	if cookiesFile == "" {
		return ""
	}
	st, err := os.Stat(cookiesFile)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", st.ModTime().UnixNano(), st.Size())
}

// cookieCopy copies the cookies for one yt-dlp run (path "" — there are
// none); done removes the copy once the run is over.
func cookieCopy() (path string, done func()) {
	nothing := func() {}
	if cookiesFile == "" {
		return "", nothing
	}
	data, err := os.ReadFile(cookiesFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("youtube cookies: %v", err)
		}
		return "", nothing
	}
	f, err := os.CreateTemp("", "kaikku-cookies-*.txt") // 0600
	if err != nil {
		log.Printf("youtube cookies: %v", err)
		return "", nothing
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		log.Printf("youtube cookies: %v", err)
		return "", nothing
	}
	return f.Name(), func() { os.Remove(f.Name()) }
}

// Check asks YouTube for videoID the way the worker does (yt-dlp itself,
// not the helper): nil means YouTube lets the server in, ErrBlocked that
// it still doesn't.
func Check(ctx context.Context, videoID string) error {
	cmd, done := command(ctx, "-j", "--no-warnings", "-f", "140/bestaudio", "https://www.youtube.com/watch?v="+videoID)
	defer done()
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("yt-dlp: %w", wrapWithStderr(err))
	}
	return nil
}
