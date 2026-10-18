package deezer

import "sync"

// Треклист альбома, радио и чарт Deezer отдают только основного
// исполнителя, а фиты есть лишь в /track/{id}. EnrichContributors
// дозапрашивает их через GetTrack (с кешем, cache.go), не больше
// tracksParallel запросов одновременно; общий темп держит limit.go.
// Ошибки не фатальны: у трека остаётся основной исполнитель.

const tracksParallel = 6

// EnrichContributors fills Contributors on tracks that don't have them.
// Сначала — из кеша одним запросом к store, к Deezer — только за остальными.
func (c *Client) EnrichContributors(tracks []Track) {
	if len(c.EnrichContributorsCached(tracks)) == 0 {
		return
	}
	sem := make(chan struct{}, tracksParallel)
	var wg sync.WaitGroup
	for i := range tracks {
		if len(tracks[i].Contributors) > 0 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t *Track) {
			defer wg.Done()
			defer func() { <-sem }()
			if full, err := c.GetTrack(t.ID); err == nil && len(full.Contributors) > 0 {
				t.Contributors = full.Contributors
			}
		}(&tracks[i])
	}
	wg.Wait()
}

// EnrichContributorsCached — то же, но только из кеша, без запросов к
// Deezer: приложения (Subsonic) открывают разом всю дискографию артиста,
// и догрузка фитов упиралась бы в лимит запросов. Возвращает ID треков,
// которых в кеше нет, — их можно догрузить фоном.
func (c *Client) EnrichContributorsCached(tracks []Track) (missing []string) {
	var ids []string
	for _, t := range tracks {
		if len(t.Contributors) == 0 {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	cached := c.tracks.getMany(ids)
	for i := range tracks {
		if len(tracks[i].Contributors) > 0 {
			continue
		}
		if full, ok := cached[tracks[i].ID]; !ok {
			missing = append(missing, tracks[i].ID)
		} else if len(full.Contributors) > 0 {
			tracks[i].Contributors = full.Contributors
		}
	}
	return missing
}

// GetTracks resolves several tracks at once, in the order of ids. The first
// error fails the whole call.
func (c *Client) GetTracks(ids []string) ([]*Track, error) {
	out := make([]*Track, len(ids))
	errs := make([]error, len(ids))
	sem := make(chan struct{}, tracksParallel)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i], errs[i] = c.GetTrack(id)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
