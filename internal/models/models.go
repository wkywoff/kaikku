package models

import "kaikku/internal/deezer"

type Artist struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	DeezerID      string  `json:"deezer_id"`
	PictureKey    *string `json:"picture_key,omitempty"`
	Edited        bool    `json:"edited"`
	PictureCustom bool    `json:"picture_custom"`
	ImageURL      string  `json:"image_url,omitempty"`
}

type Album struct {
	ID          string             `json:"id"`
	ArtistID    string             `json:"artist_id"`
	Title       string             `json:"title"`
	CoverKey    *string            `json:"cover_key,omitempty"`
	ReleasedAt  *string            `json:"released_at,omitempty"`
	DeezerID    string             `json:"deezer_id"`
	RecordType  *string            `json:"record_type,omitempty"`
	Edited      bool               `json:"edited"`
	CoverCustom bool               `json:"cover_custom"`
	ImageURL    string             `json:"image_url,omitempty"`
	Hidden      bool               `json:"hidden"`
	VersionOf   *string            `json:"version_of,omitempty"`
	Artists     []deezer.ArtistRef `json:"artists"`
}
