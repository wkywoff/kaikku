package ingest

import (
	"fmt"
	"path/filepath"
)

func PrefetchPath(stagingDir string, jobID int64) string {
	return filepath.Join(stagingDir, fmt.Sprintf("stream-%d.m4a", jobID))
}
