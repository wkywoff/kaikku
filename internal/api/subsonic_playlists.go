package api

import (
	"strconv"

	"kaikku/internal/deezer"
)

// Плейлисты для приложений — те же, что на сайте: свои, совместные и
// чужие публичные. Права — как на сайте: треки меняют владелец и
// соавторы, название и удаление — только владелец.

func init() {
	subsonicRoute("getPlaylists", subsonicGetPlaylists)
	subsonicRoute("getPlaylist", subsonicGetPlaylist)
	subsonicRoute("createPlaylist", subsonicCreatePlaylist)
	subsonicRoute("updatePlaylist", subsonicUpdatePlaylist)
	subsonicRoute("deletePlaylist", subsonicDeletePlaylist)
}

type subPlaylistInfo struct {
	ID        string    `xml:"id,attr" json:"id"`
	Name      string    `xml:"name,attr" json:"name"`
	Comment   string    `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Owner     string    `xml:"owner,attr" json:"owner"`
	Public    bool      `xml:"public,attr" json:"public"`
	SongCount int       `xml:"songCount,attr" json:"songCount"`
	Duration  int       `xml:"duration,attr" json:"duration"`
	Created   string    `xml:"created,attr" json:"created"`
	Changed   string    `xml:"changed,attr" json:"changed"`
	CoverArt  string    `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Readonly  bool      `xml:"readonly,attr" json:"readonly"` // OpenSubsonic: менять треки нельзя
	Entries   []subSong `xml:"entry" json:"entry,omitempty"`
}

func toSubPlaylist(p playlistInfo) subPlaylistInfo {
	out := subPlaylistInfo{
		ID: subPlaylist + p.ID, Name: p.Title, Comment: p.Description, Owner: p.Owner.Username, Public: p.IsPublic,
		SongCount: p.TrackCount, Duration: int(p.DurationMs / 1000), Created: subTime(p.CreatedAt), Changed: subTime(p.UpdatedAt),
		CoverArt: subPlaylist + p.ID, Readonly: !p.CanEdit,
	}
	if len(p.Covers) > 0 {
		out.CoverArt = coverID(p.Covers[0], out.CoverArt)
	}
	return out
}

func subsonicGetPlaylists(c *subCtx) error {
	var out struct {
		Mine   []playlistInfo `json:"mine"`
		Others []playlistInfo `json:"others"`
	}
	if err := c.call(c.s.handlePlaylists, "GET", "/", nil, nil, &out); err != nil {
		return err
	}
	var list []subPlaylistInfo
	for _, p := range append(out.Mine, out.Others...) {
		list = append(list, toSubPlaylist(p))
	}
	return c.ok("playlists", struct {
		Playlists []subPlaylistInfo `xml:"playlist" json:"playlist,omitempty"`
	}{list})
}

// playlist — плейлист и треки, которые видит приложение (без скрытых):
// по ним же считаются номера в updatePlaylist.
func (c *subCtx) playlist(id string) (playlistInfo, []collectionTrack, error) {
	var out struct {
		Playlist playlistInfo      `json:"playlist"`
		Tracks   []collectionTrack `json:"tracks"`
	}
	if err := c.call(c.s.handleGetPlaylist, "GET", "/", map[string]string{"id": id}, nil, &out); err != nil {
		return playlistInfo{}, nil, err
	}
	var visible []collectionTrack
	for _, t := range out.Tracks {
		if !t.Hidden {
			visible = append(visible, t)
		}
	}
	return out.Playlist, visible, nil
}

func (c *subCtx) playlistWithSongs(id string) (subPlaylistInfo, error) {
	info, items, err := c.playlist(id)
	if err != nil {
		return subPlaylistInfo{}, err
	}
	tracks := make([]deezer.Track, len(items))
	for i, it := range items {
		tracks[i] = it.Track
	}
	p := toSubPlaylist(info)
	p.Entries = c.songs(tracks)
	p.SongCount = len(p.Entries)
	return p, nil
}

func subsonicGetPlaylist(c *subCtx) error {
	id, err := c.catalogParam("id", subPlaylist)
	if err != nil {
		return err
	}
	p, err := c.playlistWithSongs(id)
	if err != nil {
		return err
	}
	return c.ok("playlist", p)
}

// songIDs — Deezer ID треков из параметра name (t…).
func (c *subCtx) songIDs(name string) ([]string, error) {
	var out []string
	for _, id := range c.params(name) {
		kind, raw := parseSubID(id)
		if kind != subTrack || !validCatalogID(raw) {
			return nil, subFail(subErrNotFound, "no such song: "+id)
		}
		out = append(out, raw)
	}
	return out, nil
}

// addSongs — в конец плейлиста, порциями, как разрешает сайт.
func (c *subCtx) addSongs(playlistID string, ids []string) error {
	for len(ids) > 0 {
		n := min(len(ids), maxPlaylistAdd)
		if err := c.call(c.s.handleAddToPlaylist, "POST", "/", map[string]string{"id": playlistID},
			map[string]any{"deezer_ids": ids[:n]}, nil); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

// createPlaylist: name — новый; playlistId — заменить треки существующего.
func subsonicCreatePlaylist(c *subCtx) error {
	songs, err := c.songIDs("songId")
	if err != nil {
		return err
	}
	var id string
	if c.param("playlistId") != "" {
		if id, err = c.catalogParam("playlistId", subPlaylist); err != nil {
			return err
		}
		_, items, err := c.playlist(id)
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := c.removeItem(id, it.ItemID); err != nil {
				return err
			}
		}
	} else {
		name := c.param("name")
		if name == "" {
			return subMissing("name")
		}
		var p playlistInfo
		if err := c.call(c.s.handleCreatePlaylist, "POST", "/", nil, map[string]any{"title": name}, &p); err != nil {
			return err
		}
		id = p.ID
	}
	if err := c.addSongs(id, songs); err != nil {
		return err
	}
	p, err := c.playlistWithSongs(id)
	if err != nil {
		return err
	}
	return c.ok("playlist", p)
}

func (c *subCtx) removeItem(playlistID string, item int64) error {
	return c.call(c.s.handleRemoveFromPlaylist, "DELETE", "/",
		map[string]string{"id": playlistID, "item": strconv.FormatInt(item, 10)}, nil, nil)
}

// updatePlaylist: название, описание и доступ (владелец), треки — добавить
// в конец и убрать по номерам (владелец и соавторы).
func subsonicUpdatePlaylist(c *subCtx) error {
	id, err := c.catalogParam("playlistId", subPlaylist)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	if v, ok := c.r.Form["name"]; ok {
		fields["title"] = v[0]
	}
	if v, ok := c.r.Form["comment"]; ok {
		fields["description"] = v[0]
	}
	if v, ok := c.r.Form["public"]; ok {
		fields["is_public"] = v[0] == "true"
	}
	if len(fields) > 0 {
		if err := c.call(c.s.handleUpdatePlaylist, "PUT", "/", map[string]string{"id": id}, fields, nil); err != nil {
			return err
		}
	}
	add, err := c.songIDs("songIdToAdd")
	if err != nil {
		return err
	}
	if remove := c.params("songIndexToRemove"); len(remove) > 0 {
		_, items, err := c.playlist(id)
		if err != nil {
			return err
		}
		for _, s := range remove {
			i, err := strconv.Atoi(s)
			if err != nil || i < 0 || i >= len(items) {
				return subFail(subErrNotFound, "no song at index "+s)
			}
			if err := c.removeItem(id, items[i].ItemID); err != nil {
				return err
			}
		}
	}
	if err := c.addSongs(id, add); err != nil {
		return err
	}
	return c.ok("", nil)
}

func subsonicDeletePlaylist(c *subCtx) error {
	id, err := c.catalogParam("id", subPlaylist)
	if err != nil {
		return err
	}
	if err := c.call(c.s.handleDeletePlaylist, "DELETE", "/", map[string]string{"id": id}, nil, nil); err != nil {
		return err
	}
	return c.ok("", nil)
}
