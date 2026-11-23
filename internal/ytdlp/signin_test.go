package ytdlp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func cookiesUploadedForTest(t *testing.T) {
	t.Helper()
	setCookiesForTest(t)
	if _, err := SaveCookies([]byte(sampleCookies)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cookiesFirstUntil.Store(0) })
}

// withCookies — какие из запусков yt-dlp были с куками.
func withCookies(calls []string) []bool {
	out := make([]bool, len(calls))
	for i, c := range calls {
		out[i] = strings.Contains(c, "--cookies ")
	}
	return out
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// С куками yt-dlp медленнее (~10 с вместо ~2): они идут в дело, только
// когда YouTube просит войти.
func TestSignIn_CookiesOnlyWhenYouTubeAsks(t *testing.T) {
	calls := fakeYtDlp(t, "")
	cookiesUploadedForTest(t)
	ctx := context.Background()

	if _, err := Download(ctx, "okA", t.TempDir(), "x"); err != nil {
		t.Fatalf("plain video: %v", err)
	}
	if _, err := Download(ctx, "ageB", t.TempDir(), "x"); err != nil {
		t.Fatalf("18+ download: %v", err)
	}
	if _, err := Resolve(ctx, "ageC"); err != nil {
		t.Fatalf("18+ extraction: %v", err)
	}
	body, err := Stream(ctx, "ageD")
	if err != nil {
		t.Fatalf("18+ stream: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "audio:ageD" {
		t.Errorf("stream read %q", data)
	}
	// okA: быстрый клиент; ageB: быстрый без кук, штатные с куками; ageC и
	// ageD: без кук, затем с ними.
	want := []bool{false, false, true, false, true, false, true}
	if got := withCookies(*calls); !equalBools(got, want) {
		t.Errorf("cookies per run %v, want %v: %q", got, want, *calls)
	}
	if cookiesFirst() {
		t.Error("an 18+ video put the cookies first for everything")
	}
}

// YouTube не пускает сервер без кук — дальше они идут первыми.
func TestSignIn_RefusedServerPutsCookiesFirst(t *testing.T) {
	calls := fakeYtDlp(t, "")
	cookiesUploadedForTest(t)
	ctx := context.Background()

	if _, err := Download(ctx, "botA", t.TempDir(), "x"); err != nil {
		t.Fatalf("refused without cookies: %v", err)
	}
	if !cookiesFirst() {
		t.Fatal("a refusal didn't put the cookies first")
	}
	if _, err := Download(ctx, "okB", t.TempDir(), "x"); err != nil {
		t.Fatalf("next download: %v", err)
	}
	if _, err := Resolve(ctx, "okC"); err != nil {
		t.Fatalf("next extraction: %v", err)
	}
	if got, want := withCookies(*calls), []bool{false, true, true, true}; !equalBools(got, want) {
		t.Errorf("cookies per run %v, want %v: %q", got, want, *calls)
	}
}

// Кук нет — без них YouTube не отдаст, лишних попыток нет; 18+ — ошибка
// этого видео, а не всего сервера.
func TestSignIn_WithoutCookies(t *testing.T) {
	calls := fakeYtDlp(t, "")
	ctx := context.Background()

	_, err := Download(ctx, "ageA", t.TempDir(), "x")
	if err == nil || errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "confirm your age") {
		t.Fatalf("18+ without cookies: %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("runs for an 18+ video: %q, want the quick and the default clients", *calls)
	}
	if _, err := Download(ctx, "botB", t.TempDir(), "x"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("refused without cookies: %v, want ErrBlocked", err)
	}
	for _, c := range *calls {
		if strings.Contains(c, "--cookies") {
			t.Errorf("cookies passed without any uploaded: %q", c)
		}
	}
}

func TestSignIn_SearchRetriesWithCookies(t *testing.T) {
	calls := fakeYtDlp(t, "NEEDCOOKIES\n"+searchLine("okA", "Song", 200))
	cookiesUploadedForTest(t)
	got, err := search(context.Background(), "ytsearch15:Artist Song")
	if err != nil || len(got) != 1 || got[0].ID != "okA" {
		t.Fatalf("search: %+v, %v", got, err)
	}
	if want := []bool{false, true}; !equalBools(withCookies(*calls), want) {
		t.Errorf("search runs %q", *calls)
	}
}

func TestCheck_AlwaysWithCookies(t *testing.T) {
	calls := fakeYtDlp(t, "")
	cookiesUploadedForTest(t)
	if err := Check(context.Background(), "okA"); err != nil {
		t.Fatal(err)
	}
	if !equalBools(withCookies(*calls), []bool{true}) {
		t.Errorf("Check ran %q, want it with the cookies", *calls)
	}
}
