package ingest

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"kaikku/internal/deezer"
)

// FillMetadata докачивает ISRC треков, скачанных до миграции 0015
// (fillISRC). Фото артистов и обложки Deezer у себя больше не храним:
// фронт берёт их через прокси (/api/img, internal/api/images.go).
// Best-effort: ошибки только логируются, следующий вызов попробует снова.
// Вызывается при старте воркера и после каждого готового job'а.
func FillMetadata(ctx context.Context, db *sql.DB, dz *deezer.Client) {
	fillISRC(ctx, db, dz)
}

// fillISRC узнаёт у Deezer ISRC треков, скачанных до миграции 0015: по нему
// воркер находит ту же запись на другом релизе. Разом — до 200 треков (лимит
// запросов у клиента воркера свой); ” — у Deezer его нет или трек удалён.
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
		case errors.As(err, &apiErr) && apiErr.Code == 800: // трека на Deezer больше нет
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
