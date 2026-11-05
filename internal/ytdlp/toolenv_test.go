package ytdlp

import (
	"context"
	"slices"
	"testing"
)

func TestToolEnvDropsSecrets(t *testing.T) {
	t.Setenv("JWT_SECRET", "x")
	t.Setenv("MEDIA_SECRET", "x")
	t.Setenv("DATABASE_URL", "x")
	t.Setenv("POSTGRES_PASSWORD", "x")
	t.Setenv("LASTFM_API_KEY", "x")
	t.Setenv("PYTHONPATH", "/ytdlp")
	env := execCommand(context.Background(), "true").Env
	for _, secret := range []string{"JWT_SECRET=x", "MEDIA_SECRET=x", "DATABASE_URL=x", "POSTGRES_PASSWORD=x", "LASTFM_API_KEY=x"} {
		if slices.Contains(env, secret) {
			t.Errorf("%s reached the tool", secret)
		}
	}
	if !slices.Contains(env, "PYTHONPATH=/ytdlp") {
		t.Error("the tool lost PYTHONPATH")
	}
}
