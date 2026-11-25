package api

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/safe"
)

var (
	enrichLimit atomic.Pointer[deezer.Limiter]
	enrichMu    sync.Mutex
	enrichQueue = map[string]*deezer.Client{}
	enrichWake  = make(chan struct{}, 1)
	enrichOnce  sync.Once
)

func init() { enrichLimit.Store(deezer.NewLimiter(3, 5*time.Second)) }

const enrichQueueMax = 5000

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
