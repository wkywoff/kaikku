package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sync"

	"kaikku/internal/deezer"
)

// Сведение импортированных треков на один релиз.
//
// Поиск Deezer отдаёт одну версию записи из всех, где она есть (альбом,
// делюкс, сингл, сборник), и какую — решает сам: из одного плейлиста одна
// песня альбома попадает на обычное издание, другая — на делюкс, третья — на
// сборник. Когда все треки найдены, фронт присылает их сюда, и треки одного
// исполнителя переезжают на релиз, где их больше всего. Ту же запись на
// другом релизе узнаём по ISRC (clean- и explicit-версии — разные записи,
// разные ISRC, так что версия не меняется).
//
// Кандидаты — только релизы, куда уже попал какой-то трек плейлиста: так
// хватает нескольких запросов к Deezer, а не всей дискографии.

const (
	maxImportAlbumsTracks = 300 // как MAX_IMPORT на фронте
	maxImportAlbums       = 40  // релизов на запрос: по два запроса к Deezer на каждый
)

func (s *Server) handleImportAlbums(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tracks []*deezer.Track `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Tracks) == 0 || len(req.Tracks) > maxImportAlbumsTracks {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("tracks: 1–%d songs are required", maxImportAlbumsTracks))
		return
	}
	releases := s.importReleases(albumsToSettle(req.Tracks))
	writeJSON(w, http.StatusOK, map[string]any{"matches": settleAlbums(req.Tracks, releases)})
}

// importRelease — релиз-кандидат: тип и треклист (nil — Deezer не ответил).
type importRelease struct {
	recordType string
	tracks     []deezer.Track
}

// albumsToSettle — релизы, которые стоит сравнить: у исполнителя в плейлисте
// треки с разных релизов.
func albumsToSettle(tracks []*deezer.Track) map[string]bool {
	byArtist := map[string]map[string]bool{}
	var order []string
	for _, t := range tracks {
		if t == nil || t.ISRC == "" || t.ArtistID == "" || t.AlbumID == "" {
			continue
		}
		if byArtist[t.ArtistID] == nil {
			byArtist[t.ArtistID] = map[string]bool{}
			order = append(order, t.ArtistID)
		}
		byArtist[t.ArtistID][t.AlbumID] = true
	}
	out := map[string]bool{}
	for _, artist := range order {
		albums := byArtist[artist]
		if len(albums) < 2 || len(out)+len(albums) > maxImportAlbums {
			continue
		}
		for id := range albums {
			out[id] = true
		}
	}
	return out
}

// importReleases загружает тип и треклист каждого релиза (из кеша или у
// Deezer, в пределах лимита импорта). Ошибка Deezer не роняет импорт:
// такой релиз просто не участвует в сравнении.
func (s *Server) importReleases(albums map[string]bool) map[string]importRelease {
	out := make(map[string]importRelease, len(albums))
	var mu sync.Mutex
	slots := make(chan struct{}, importParallel)
	var wg sync.WaitGroup
	for id := range albums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			var rel importRelease
			err := guard("import albums", func() error {
				for range s.Deezer.AlbumRequests(id, 200) {
					importLimit.Wait()
				}
				album, err := s.Deezer.GetAlbum(id)
				if err != nil {
					return err
				}
				rel.recordType = album.RecordType
				rel.tracks, err = s.Deezer.GetAlbumTracks(album, 200)
				return err
			})
			if err != nil {
				log.Printf("import albums: album %s: %v", id, err)
				return
			}
			mu.Lock()
			out[id] = rel
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// releaseClass: сначала обычные альбомы и EP, потом синглы, потом сборники
// (и всё неизвестное) — песня с альбома не должна переезжать на «Best of».
func releaseClass(recordType string) int {
	switch recordType {
	case "album", "ep":
		return 0
	case "single":
		return 1
	}
	return 2
}

// settleAlbums возвращает matches, где треки одного исполнителя, по
// возможности, с одного релиза. Кандидаты для трека — релизы, куда попали
// треки того же исполнителя и где есть его запись; из них выбирается:
// альбом, а не сингл или сборник; где больше записей исполнителя из
// плейлиста; обычное издание, а не делюкс (меньше треков). При прочем
// равном трек остаётся, где был.
func settleAlbums(matches []*deezer.Track, releases map[string]importRelease) []*deezer.Track {
	ids := make([]string, 0, len(releases)) // по порядку — чтобы выбор не зависел от обхода map
	byISRC := map[string]map[string]*deezer.Track{}
	for id, rel := range releases {
		ids = append(ids, id)
		m := map[string]*deezer.Track{}
		for i := range rel.tracks {
			if isrc := rel.tracks[i].ISRC; isrc != "" && m[isrc] == nil {
				m[isrc] = &rel.tracks[i]
			}
		}
		byISRC[id] = m
	}
	slices.Sort(ids)

	type key struct{ artist, album string }
	landed := map[key]bool{}       // сюда попал трек исполнителя
	isrcs := map[string][]string{} // исполнитель → его записи в плейлисте
	for _, t := range matches {
		if t == nil || t.ISRC == "" {
			continue
		}
		landed[key{t.ArtistID, t.AlbumID}] = true
		if !slices.Contains(isrcs[t.ArtistID], t.ISRC) {
			isrcs[t.ArtistID] = append(isrcs[t.ArtistID], t.ISRC)
		}
	}
	hits := map[key]int{}
	for k := range landed {
		for _, isrc := range isrcs[k.artist] {
			if byISRC[k.album][isrc] != nil {
				hits[k]++
			}
		}
	}

	out := make([]*deezer.Track, len(matches))
	for i, t := range matches {
		out[i] = t
		if t == nil || t.ISRC == "" {
			continue
		}
		if _, ok := releases[t.AlbumID]; !ok {
			continue // свой релиз не загрузился — сравнивать не с чем
		}
		better := func(a, b string) bool { // a лучше b
			ra, rb := releases[a], releases[b]
			if ca, cb := releaseClass(ra.recordType), releaseClass(rb.recordType); ca != cb {
				return ca < cb
			}
			if ha, hb := hits[key{t.ArtistID, a}], hits[key{t.ArtistID, b}]; ha != hb {
				return ha > hb
			}
			return len(ra.tracks) < len(rb.tracks)
		}
		best := t.AlbumID
		for _, id := range ids {
			if id != best && landed[key{t.ArtistID, id}] && byISRC[id][t.ISRC] != nil && better(id, best) {
				best = id
			}
		}
		if best == t.AlbumID {
			continue
		}
		moved := *byISRC[best][t.ISRC]
		if len(moved.Contributors) == 0 {
			moved.Contributors = t.Contributors // та же запись — те же исполнители
		}
		out[i] = &moved
	}
	return out
}
