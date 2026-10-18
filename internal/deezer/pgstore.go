package deezer

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/lib/pq"
)

// PGStore — Store в таблице deezer_cache (миграция 0033). Ошибки базы —
// только в лог: без кеша Deezer всё равно ответит. Чтение ждёт базу
// недолго: тормозит база — значит, промах, а не зависший запрос.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db} }

const (
	storeReadTimeout  = 500 * time.Millisecond
	storeWriteTimeout = 2 * time.Second
)

func (s *PGStore) Get(key string) ([]byte, time.Time, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), storeReadTimeout)
	defer cancel()
	var data []byte
	var expires time.Time
	err := s.db.QueryRowContext(ctx, `SELECT data, expires_at FROM deezer_cache WHERE key = $1 AND expires_at > now()`, key).
		Scan(&data, &expires)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("deezer cache %s: %v", key, err)
		}
		return nil, time.Time{}, false
	}
	return data, expires, true
}

func (s *PGStore) GetMany(keys []string) map[string]StoreEntry {
	ctx, cancel := context.WithTimeout(context.Background(), storeReadTimeout)
	defer cancel()
	out := map[string]StoreEntry{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, data, expires_at FROM deezer_cache WHERE key = ANY($1) AND expires_at > now()`, pq.Array(keys))
	if err != nil {
		log.Printf("deezer cache: %d keys: %v", len(keys), err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var e StoreEntry
		if rows.Scan(&key, &e.Data, &e.Expires) == nil {
			out[key] = e
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("deezer cache: %d keys: %v", len(keys), err)
	}
	return out
}

func (s *PGStore) Put(key string, data []byte, expires time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), storeWriteTimeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO deezer_cache (key, data, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET data = EXCLUDED.data, expires_at = EXCLUDED.expires_at`,
		key, data, expires,
	); err != nil {
		log.Printf("deezer cache %s: %v", key, err)
	}
}

func (s *PGStore) Forget(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), storeWriteTimeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM deezer_cache WHERE key = $1 OR starts_with(key, $1 || ':')`, key); err != nil {
		log.Printf("deezer cache forget %s: %v", key, err)
	}
}
