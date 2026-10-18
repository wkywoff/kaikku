package api

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/safe"
)

// Фиты треков для приложений (Subsonic) и поиска. Треклист альбома и
// выдача поиска у Deezer — без исполнителей, за ними нужен запрос на
// каждый трек; Feishin открывает разом всю дискографию артиста, поиск
// печатают на ходу, и это съедало лимит Deezer. Поэтому там фиты — из
// кеша (у альбома, если трека там нет, — исполнители альбома,
// guessContributors), а трек догружается фоном малой долей лимита: со
// следующего раза — точно.

// Фоновая догрузка — 3 запроса за 5 с: сайту и приложениям остаётся своё.
var (
	enrichLimit atomic.Pointer[deezer.Limiter] // тесты ставят свой
	enrichMu    sync.Mutex
	enrichQueue = map[string]*deezer.Client{} // ждут догрузки: трек → чей клиент
	enrichWake  = make(chan struct{}, 1)
	enrichOnce  sync.Once
)

func init() { enrichLimit.Store(deezer.NewLimiter(3, 5*time.Second)) }

// enrichQueueMax — не больше стольких треков в очереди: остальные
// догрузятся в следующий раз.
const enrichQueueMax = 5000

// guessContributors — исполнители альбома тем трекам, чьих исполнителей
// нет в кеше: у совместного релиза (несколько главных исполнителей)
// трек, чей исполнитель среди них, почти всегда — всех их.
func guessContributors(album *deezer.Album, tracks []deezer.Track) {
	if len(album.Contributors) < 2 {
		return
	}
	for i := range tracks {
		t := &tracks[i]
		if len(t.Contributors) > 0 {
			continue
		}
		for _, c := range album.Contributors {
			if c.ID == t.ArtistID {
				t.Contributors = append([]deezer.ArtistRef(nil), album.Contributors...)
				break
			}
		}
	}
}

// enrichLater ставит треки, которых нет в кеше (их вернул
// EnrichContributorsCached), на фоновую догрузку.
func (s *Server) enrichLater(ids []string) {
	if len(ids) == 0 {
		return
	}
	enrichMu.Lock()
	added := false
	for _, id := range ids {
		if len(enrichQueue) >= enrichQueueMax {
			break
		}
		if _, queued := enrichQueue[id]; validDeezerID(id) && !queued {
			enrichQueue[id] = s.Deezer
			added = true
		}
	}
	enrichMu.Unlock()
	if !added {
		return
	}
	enrichOnce.Do(func() { safe.Go("enrich tracks", enrichLoop) })
	select {
	case enrichWake <- struct{}{}:
	default:
	}
}

func enrichLoop() {
	for range enrichWake {
		for {
			enrichMu.Lock()
			var id string
			var dz *deezer.Client
			for id, dz = range enrichQueue {
				break
			}
			enrichMu.Unlock()
			if dz == nil {
				break
			}
			if !dz.HasTrack(id) {
				enrichLimit.Load().Wait()
				if _, err := dz.GetTrack(id); err != nil {
					log.Printf("enrich track %s: %v", id, err)
				}
			}
			enrichMu.Lock()
			delete(enrichQueue, id)
			enrichMu.Unlock()
		}
	}
}
