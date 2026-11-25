package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var cookiesFile string

func haveCookies() bool {
	if cookiesFile == "" {
		return false
	}
	_, err := os.Stat(cookiesFile)
	return err == nil
}

const cookiesFirstFor = 30 * time.Minute

var cookiesFirstUntil atomic.Int64

func cookiesFirst() bool {
	return time.Now().UnixNano() < cookiesFirstUntil.Load() && haveCookies()
}

func signInNeeded(output string) bool {
	if !needsSignIn(output) || !haveCookies() {
		return false
	}
	if isBlocked(output) {
		cookiesFirstUntil.Store(time.Now().Add(cookiesFirstFor).UnixNano())
	}
	return true
}

func stderrOf(err error) string {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(exitErr.Stderr)
	}
	return ""
}

func ytdlpOutput(ctx context.Context, args ...string) ([]byte, error) {
	signIn := cookiesFirst()
	for {
		cmd, done := command(ctx, signIn, args...)
		out, err := cmd.Output()
		done()
		if err == nil || signIn || ctx.Err() != nil || !signInNeeded(stderrOf(err)) {
			return out, err
		}
		signIn = true
	}
}

func SetCookiesFile(path string) { cookiesFile = path }

func CookiesPath(stagingDir string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(stagingDir)), "youtube-cookies.txt")
}

type CookiesInfo struct {
	UpdatedAt time.Time  `json:"updated_at"`
	Count     int        `json:"count"`
	SignedIn  bool       `json:"signed_in"`
	Expires   *time.Time `json:"expires,omitempty"`
}

var (
	ErrCookiesJSON      = errors.New("the cookies are JSON, not a cookies.txt file (Netscape format)")
	ErrCookiesFormat    = errors.New("not a cookies.txt file (Netscape format)")
	ErrNoYouTubeCookies = errors.New("no youtube.com cookies in the file")
	ErrCookiesOff       = errors.New("youtube cookies are off on this server")
)

var signInCookies = map[string]bool{
	"SID": true, "__Secure-1PSID": true, "__Secure-3PSID": true,
	"SAPISID": true, "__Secure-3PAPISID": true, "LOGIN_INFO": true,
}

func parseCookies(data []byte) ([]byte, CookiesInfo, error) {
	var info CookiesInfo
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if t := bytes.TrimSpace(data); len(t) > 0 && (t[0] == '[' || t[0] == '{') {
		return nil, info, ErrCookiesJSON
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

func SaveCookies(data []byte) (CookiesInfo, error) {
	if cookiesFile == "" {
		return CookiesInfo{}, ErrCookiesOff
	}
	clean, info, err := parseCookies(data)
	if err != nil {
		return info, err
	}
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
	f, err := os.CreateTemp("", "kaikku-cookies-*.txt")
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

func Check(ctx context.Context, videoID string) error {
	cmd, done := command(ctx, true, "-j", "--no-warnings", "-f", "140/bestaudio", "https://www.youtube.com/watch?v="+videoID)
	defer done()
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("yt-dlp: %w", wrapWithStderr(err))
	}
	return nil
}
