package deezer

import "sync"

const tracksParallel = 6

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
