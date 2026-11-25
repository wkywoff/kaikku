package ytdlp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeYtDlpModule = `import json
import os


class YoutubeDL:
    def __init__(self, params=None):
        self.params = params or {}

    def extract_info(self, url, download=False):
        signed_in = bool(self.params.get('cookiefile'))
        with open(os.environ['FAKE_YTDLP_LOG'], 'a') as f:
            f.write(json.dumps({'url': url, 'cookies': signed_in}) + '\n')
        if url.startswith('ytsearch'):
            if 'botsearch' in url and not signed_in:
                self.params['logger'].error("ERROR: [youtube:search] Sign in to confirm you're not a bot")
                return None
            return {'entries': [{'id': 'okA', 'title': 'Song', 'channel': 'A - Topic', 'duration': 200}]}
        video = url.rsplit('=', 1)[-1]
        if not signed_in and video.startswith('age'):
            raise Exception('ERROR: [youtube] %s: Sign in to confirm your age. This video may be inappropriate '
                            'for some users. Use --cookies-from-browser or --cookies for the authentication.' % video)
        if not signed_in and video.startswith('bot'):
            raise Exception("ERROR: [youtube] %s: Sign in to confirm you're not a bot. Use --cookies-from-browser "
                            'or --cookies for the authentication.' % video)
        return {'url': 'https://media.test/' + video + ('?signed-in' if signed_in else ''),
                'protocol': 'https', 'http_headers': {}}
`

type helperCall struct {
	URL     string `json:"url"`
	Cookies bool   `json:"cookies"`
}

func startHelperPy(t *testing.T, withCookies bool) (ask func(req string) map[string]any, calls func() []helperCall) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "yt_dlp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "yt_dlp", "__init__.py"), []byte(fakeYtDlpModule), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.jsonl")
	cmd := exec.Command(python, "-c", helperScript)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+dir, "FAKE_YTDLP_LOG="+logPath)
	if withCookies {
		cookies := filepath.Join(dir, "cookies.txt")
		if err := os.WriteFile(cookies, []byte(sampleCookies), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(cmd.Env, "KAIKKU_YTDLP_COOKIES="+cookies)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Wait()
	})
	replies := bufio.NewScanner(stdout)
	id := 0
	ask = func(req string) map[string]any {
		t.Helper()
		id++
		if _, err := fmt.Fprintf(stdin, `{"id": %d, %s}`+"\n", id, req); err != nil {
			t.Fatal(err)
		}
		if !replies.Scan() {
			t.Fatalf("helper.py: no reply to %s", req)
		}
		var r map[string]any
		if err := json.Unmarshal(replies.Bytes(), &r); err != nil {
			t.Fatalf("helper.py reply %q: %v", replies.Text(), err)
		}
		return r
	}
	calls = func() []helperCall {
		f, err := os.Open(logPath)
		if err != nil {
			return nil
		}
		defer f.Close()
		var out []helperCall
		for dec := json.NewDecoder(f); ; {
			var c helperCall
			if err := dec.Decode(&c); err == io.EOF {
				return out
			} else if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
	}
	return ask, calls
}

func TestHelperPy_CookiesOnlyWhenYouTubeAsks(t *testing.T) {
	ask, calls := startHelperPy(t, true)
	url := func(r map[string]any) string { s, _ := r["url"].(string); return s }

	if r := ask(`"op": "extract", "video": "okA"`); url(r) != "https://media.test/okA" {
		t.Fatalf("plain video: %v", r)
	}
	if c := calls(); len(c) != 1 || c[0].Cookies {
		t.Fatalf("plain video ran %+v, want once without the cookies", c)
	}
	if r := ask(`"op": "extract", "video": "ageB"`); url(r) != "https://media.test/ageB?signed-in" {
		t.Fatalf("18+ video: %v", r)
	}
	if r := ask(`"op": "search", "query": "ytsearch15:botsearch"`); r["error"] != nil || len(r["entries"].([]any)) != 1 {
		t.Fatalf("refused search: %v", r)
	}

	if r := ask(`"op": "extract", "video": "botC"`); url(r) != "https://media.test/botC?signed-in" {
		t.Fatalf("refused server: %v", r)
	}
	n := len(calls())
	if r := ask(`"op": "extract", "video": "okD"`); url(r) != "https://media.test/okD?signed-in" {
		t.Fatalf("after a refusal: %v", r)
	}
	if c := calls()[n:]; len(c) != 1 || !c[0].Cookies {
		t.Errorf("after a refusal okD ran %+v, want once with the cookies", c)
	}
}

func TestHelperPy_WithoutCookies(t *testing.T) {
	ask, calls := startHelperPy(t, false)
	if r := ask(`"op": "extract", "video": "ageB"`); !strings.Contains(fmt.Sprint(r["error"]), "confirm your age") {
		t.Fatalf("18+ without cookies: %v", r)
	}
	if r := ask(`"op": "search", "query": "ytsearch15:botsearch"`); !strings.Contains(fmt.Sprint(r["error"]), "not a bot") {
		t.Fatalf("refused search without cookies: %v, want an error, not an empty result", r)
	}
	for _, c := range calls() {
		if c.Cookies {
			t.Errorf("cookies used without any: %+v", c)
		}
	}
}
