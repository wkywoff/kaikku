package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"strings"
	"testing"
)

// Эталонные значения PBKDF2-HMAC-SHA256 (как у RFC 7914 / Python hashlib).
func TestDerive_KnownVectors(t *testing.T) {
	cases := []struct {
		password, salt string
		iter           int
		want           string
	}{
		{"password", "salt", 1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{"password", "salt", 4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
		// Пароль длиннее блока HMAC (64 байта) — ключ HMAC сначала хешируется.
		{strings.Repeat("x", 100), "salt", 3, "59bfa49750dd5462ce38370a1e7abe0736ff334cf1c2f0d84f23a03435d660d1"},
	}
	for _, c := range cases {
		got, err := derive(c.password, []byte(c.salt), c.iter, 32)
		if err != nil || hex.EncodeToString(got) != c.want {
			t.Errorf("derive(%.10q…, %d) = %x, %v; want %s", c.password, c.iter, got, err, c.want)
		}
	}
}

// legacyPBKDF2 — прежняя самописная реализация (до этого коммита): хеши,
// сохранённые ею, должны проверяться и дальше.
func legacyPBKDF2(password string, salt []byte, iterations int) []byte {
	prf := func() hash.Hash { return hmac.New(sha256.New, []byte(password)) }
	h := prf()
	h.Write(salt)
	h.Write([]byte{0, 0, 0, 1})
	u := h.Sum(nil)
	result := append([]byte(nil), u...)
	for i := 1; i < iterations; i++ {
		hh := prf()
		hh.Write(u)
		u = hh.Sum(nil)
		for j := range result {
			result[j] ^= u[j]
		}
	}
	return result
}

func TestDerive_MatchesTheOldImplementation(t *testing.T) {
	for _, pw := range []string{"testpass123", strings.Repeat("пароль", 20)} {
		salt := []byte("0123456789abcdef")
		got, err := derive(pw, salt, 1000, 32)
		if err != nil || hex.EncodeToString(got) != hex.EncodeToString(legacyPBKDF2(pw, salt, 1000)) {
			t.Errorf("new and old PBKDF2 differ for %q", pw)
		}
	}
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword("testpass123", h); !ok || err != nil {
		t.Errorf("right password: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword("wrongpass", h); ok {
		t.Error("wrong password accepted")
	}
}

func TestTooLongPasswordIsRefusedWithoutHashing(t *testing.T) {
	long := strings.Repeat("a", MaxPasswordLen+1)
	if _, err := HashPassword(long); err != ErrPasswordTooLong {
		t.Errorf("HashPassword: %v, want ErrPasswordTooLong", err)
	}
	if ok, err := VerifyPassword(long, dummyHash); ok || err != nil {
		t.Errorf("VerifyPassword: %v %v, want a plain no", ok, err)
	}
}

func TestDummyHashIsValid(t *testing.T) {
	if ok, err := VerifyPassword("timing-equalizer", dummyHash); !ok || err != nil {
		t.Errorf("dummyHash doesn't verify: %v %v", ok, err)
	}
}
