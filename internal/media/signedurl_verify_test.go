package media

import (
	"net/url"
	"testing"
	"time"
)

// Ожидаемое значение получено рецептом из документации nginx secure_link:
//
//	echo -n "1893456000/media/abc123.ogg testsecret" | openssl md5 -binary | openssl base64 | tr +/ -_ | tr -d =
func TestSignMatchesNginxRecipe(t *testing.T) {
	const want = "7pf87-63614HEsGP0hTkyA"
	if got := sign(1893456000, "/media/abc123.ogg", "testsecret"); got != want {
		t.Errorf("sign() = %q, want %q (must match nginx secure_link_md5)", got, want)
	}
}

func TestPathURLVerify(t *testing.T) {
	s := &Signer{Secret: "testsecret", TTL: time.Minute}
	link, _ := s.PathURL("/api/stream/deezer/42")
	u, err := url.Parse(link)
	if err != nil || u.Path != "/api/stream/deezer/42" {
		t.Fatalf("PathURL() = %q", link)
	}
	q := u.Query()
	if err := s.Verify(u.Path, q.Get("st"), q.Get("e")); err != nil {
		t.Errorf("Verify(own link) = %v", err)
	}
	if err := s.Verify("/api/stream/deezer/43", q.Get("st"), q.Get("e")); err != ErrBadSignature {
		t.Errorf("Verify(other path) = %v, want ErrBadSignature", err)
	}
	if err := s.Verify(u.Path, q.Get("st"), q.Get("e")+"0"); err != ErrBadSignature {
		t.Errorf("Verify(changed expiry) = %v, want ErrBadSignature", err)
	}
	if err := s.Verify(u.Path, "", q.Get("e")); err != ErrBadSignature {
		t.Errorf("Verify(no st) = %v, want ErrBadSignature", err)
	}

	expired := &Signer{Secret: "testsecret", TTL: -time.Minute}
	link, _ = expired.PathURL("/x")
	u, _ = url.Parse(link)
	if err := s.Verify("/x", u.Query().Get("st"), u.Query().Get("e")); err != ErrExpired {
		t.Errorf("Verify(expired) = %v, want ErrExpired", err)
	}
}
