package api

import (
	"errors"
	"testing"
)

func TestCatalogIDs(t *testing.T) {
	cases := []struct {
		id           string
		deezer, mine bool
	}{
		{"3135556", true, false},
		{"k1", false, true},
		{"k1234567", false, true},
		{"k0", false, false},
		{"k01", false, false},
		{"k", false, false},
		{"K1", false, false},
		{"k1x", false, false},
		{"-1", false, false},
		{"", false, false},
		{"k1234567890123456789", false, false},
	}
	for _, c := range cases {
		if got := validDeezerID(c.id); got != c.deezer {
			t.Errorf("validDeezerID(%q) = %v", c.id, got)
		}
		if got := isLocalID(c.id); got != c.mine {
			t.Errorf("isLocalID(%q) = %v", c.id, got)
		}
		if got := validCatalogID(c.id); got != (c.deezer || c.mine) {
			t.Errorf("validCatalogID(%q) = %v", c.id, got)
		}
	}
}

// Свой трек не ставится на скачивание: ни запроса к Deezer, ни job.
func TestEnqueue_RefusesOwnID(t *testing.T) {
	db, _, _, srv := newTestServer(t)
	if _, err := srv.enqueueTrack("", "k5"); !errors.Is(err, errLocalID) {
		t.Fatalf("enqueueTrack(k5) err = %v, want errLocalID", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs`).Scan(&n)
	if n != 0 {
		t.Errorf("%d jobs created", n)
	}
}
