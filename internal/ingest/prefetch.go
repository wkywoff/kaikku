package ingest

import (
	"fmt"
	"path/filepath"
)

// PrefetchPath is where the API leaves the audio it downloaded while
// streaming a track that wasn't in the library yet (internal/api/stream.go):
// the raw yt-dlp output (format 140), the same thing ytdlp.Download would
// fetch. processJob takes it instead of searching and downloading again.
func PrefetchPath(stagingDir string, jobID int64) string {
	return filepath.Join(stagingDir, fmt.Sprintf("stream-%d.m4a", jobID))
}
