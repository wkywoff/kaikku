package ingest

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"kaikku/internal/deezer"
)

func FillMetadata(ctx context.Context, db *sql.DB, dz *deezer.Client) {
	fillISRC(ctx, db, dz)
}

func fillISRC(ctx context.Context, db *sql.DB, dz *deezer.Client) {
	rows, err := db.QueryContext(ctx, `SELECT id, deezer_id FROM tracks WHERE isrc IS NULL AND deezer_id ~ '^[0-9]+$' LIMIT 200`)
	if err != nil {
		log.Printf("metadata: isrc query: %v", err)
		return
	}
	type pending struct{ id, deezerID string }
	var tracks []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.deezerID) == nil {
			tracks = append(tracks, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("metadata: isrc query: reading rows: %v", err)
	}

	filled := 0
	for _, t := range tracks {
		if ctx.Err() != nil {
			return
		}
		isrc := ""
		tr, err := dz.GetTrack(t.deezerID)
		var apiErr *deezer.APIError
		switch {
		case err == nil:
			isrc = tr.ISRC
		case errors.As(err, &apiErr) && apiErr.Code == 800:
		default:
			log.Printf("metadata: isrc of track %s: %v", t.deezerID, err)
			continue
		}
		if _, err := db.Exec(`UPDATE tracks SET isrc = $2 WHERE id = $1`, t.id, isrc); err == nil {
			filled++
		}
	}
	if filled > 0 {
		log.Printf("metadata: ISRC filled for %d tracks", filled)
	}
}
