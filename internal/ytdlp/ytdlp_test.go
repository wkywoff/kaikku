package ytdlp

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestParseEntries_BasicFields(t *testing.T) {
	jsonLines := `{"id":"vid1","title":"Test Song","channel":"Test Channel","duration":210.5}`
	results := parseEntries(jsonLines)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.ID != "vid1" || r.Title != "Test Song" || r.Channel != "Test Channel" {
		t.Errorf("unexpected result: %+v", r)
	}
	if r.DurationMs != 210500 {
		t.Errorf("DurationMs = %d, want 210500", r.DurationMs)
	}
}

func TestParseEntries_ChannelFallsBackToUploader(t *testing.T) {
	results := parseEntries(`{"id":"vid1","title":"Song","uploader":"Uploader Name","duration":200}`)
	if results[0].Channel != "Uploader Name" {
		t.Errorf("Channel = %q, want fallback to uploader", results[0].Channel)
	}
}

func TestParseEntries_MultipleLinesSkipsMalformed(t *testing.T) {
	jsonLines := "not json\n" +
		`{"id":"vid1","title":"First","channel":"A","duration":100}` + "\n" +
		"{broken\n" +
		`{"id":"vid2","title":"Second","channel":"B","duration":200}`
	results := parseEntries(jsonLines)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (malformed lines skipped)", len(results))
	}
	if results[0].ID != "vid1" || results[1].ID != "vid2" {
		t.Errorf("unexpected order/ids: %+v", results)
	}
}

func TestParseEntries_EmptyInput(t *testing.T) {
	if results := parseEntries(""); len(results) != 0 {
		t.Errorf("got %d results for empty input, want 0", len(results))
	}
}

func TestPickBestMatch_PicksClosestDurationAmongEquals(t *testing.T) {
	candidates := []candidate{
		{ID: "aaa111", Title: "Right Song", DurationMs: 243000},
		{ID: "bbb222", Title: "Right Song", DurationMs: 240500},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "X", Title: "Right Song", DurationMs: 240000}, 4000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bbb222" {
		t.Errorf("got %q, want %q (closest duration wins when nothing else differs)", got, "bbb222")
	}
}

func TestPickBestMatch_PrefersTopicChannelOverCloserDuration(t *testing.T) {
	candidates := []candidate{
		{ID: "fan", Title: "Artist - Right Song (Official Video)", Channel: "SomeUploader", DurationMs: 240000},
		{ID: "topic", Title: "Right Song", Channel: "Artist - Topic", DurationMs: 241500},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "Artist", Title: "Right Song", DurationMs: 240000}, 4000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "topic" {
		t.Errorf("got %q, want the auto-generated Topic upload", got)
	}
}

func TestPickBestMatch_PenalizesOtherVersions(t *testing.T) {
	candidates := []candidate{
		{ID: "karaoke", Title: "Right Song (Karaoke Version)", Channel: "Karaoke Hits", DurationMs: 240000},
		{ID: "spedup", Title: "Right Song - Sped Up", Channel: "Artist", DurationMs: 240100},
		{ID: "real", Title: "Artist - Right Song", Channel: "ArtistVEVO", DurationMs: 242500},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "Artist", Title: "Right Song", DurationMs: 240000}, 4000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "real" {
		t.Errorf("got %q, want %q (karaoke/sped up must lose despite closer duration)", got, "real")
	}
}

// Реальный случай (Daft Punk — The Game of Love, 322 с): официальная
// заливка (332 с) вне допуска, а в допуске — Drumless Edition с
// официального канала и lyric-видео с оригинальным звуком.
func TestPickBestMatch_DrumlessEditionLoses(t *testing.T) {
	candidates := []candidate{
		{ID: "ajGKWk0auOc", Title: "Daft Punk - The Game of Love (Official Audio)", Channel: "Daft Punk", DurationMs: 332000},
		{ID: "PbCHEaWIkiM", Title: "Daft Punk - The Game Of Love - Unofficial Music Video", Channel: "Daft Punked", DurationMs: 273000},
		{ID: "SSM4fCuCeAk", Title: "Daft Punk - The Game of Love (Drumless Edition) (Official Audio)", Channel: "Daft Punk", DurationMs: 323000},
		{ID: "CkZmoa_mqLk", Title: "Daft Punk - The Game of Love [Video Lyrics]", Channel: "Paolo Lyrics", DurationMs: 323000},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "Daft Punk", Title: "The Game of Love", DurationMs: 322000}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got != "CkZmoa_mqLk" {
		t.Errorf("got %q, want the lyric video with the original audio", got)
	}
}

func TestPickBestMatch_OnlyOtherVersionsIsAnError(t *testing.T) {
	candidates := []candidate{
		{ID: "SSM4fCuCeAk", Title: "Daft Punk - The Game of Love (Drumless Edition) (Official Audio)", Channel: "Daft Punk", DurationMs: 323000},
	}
	if got, err := pickBestMatch(candidates, Want{Artist: "Daft Punk", Title: "The Game of Love", DurationMs: 321000}, 4000); err == nil {
		t.Fatalf("got %q, want an error rather than the drumless edition", got)
	}
}

func TestPickBestMatch_NoPenaltyWhenDeezerTitleHasMarker(t *testing.T) {
	candidates := []candidate{
		{ID: "studio", Title: "Right Song", Channel: "Artist - Topic", DurationMs: 300000},
		{ID: "remix", Title: "Right Song (Club Remix)", Channel: "Artist - Topic", DurationMs: 360000},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "Artist", Title: "Right Song (Club Remix)", DurationMs: 360000}, 4000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "remix" {
		t.Errorf("got %q, want %q", got, "remix")
	}
}

func TestPickBestMatch_WordBoundaries(t *testing.T) {
	// "alive" must not be read as the "live" marker.
	candidates := []candidate{{ID: "ok", Title: "Stayin Alive", Channel: "Bee Gees - Topic", DurationMs: 285000}}
	got, err := pickBestMatch(candidates, Want{Artist: "Bee Gees", Title: "Stayin' Alive", DurationMs: 285000}, 4000)
	if err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Случай с YouTube (rokma — альбом TIDE): трек находится только запросом
// "артист альбом", а в той же выдаче есть другие треки альбома с того же
// канала — их длительность может попасть в допуск, название — нет.
func TestPickBestMatch_AlbumQueryNeedsMatchingTitle(t *testing.T) {
	candidates := []candidate{
		{ID: "fakeEp00001", Title: "rokma — TIDE [FULL EP, 2025]", Channel: "rokma", DurationMs: 1140000},
		{ID: "other", Title: "Сбор КАМНЕЙ", Channel: "rokma", DurationMs: 158500},
		{ID: "fakeTrack01", Title: "Theoneandonlyhunger", Channel: "rokma", DurationMs: 159000},
	}
	got, err := pickBestMatch(candidates, Want{Artist: "rokma", Title: "THEONEANDONLYHUNGER", Album: "TIDE", DurationMs: 158000}, 4000)
	if err != nil || got != "fakeTrack01" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := pickBestMatch(candidates[:2], Want{Artist: "rokma", Title: "THEONEANDONLYHUNGER", DurationMs: 158000}, 4000); err == nil {
		t.Fatal("a different track of the same album must not match on duration alone")
	}
}

func TestRankCandidates_OrdersFallbacks(t *testing.T) {
	candidates := []candidate{
		{ID: "lyrics", Title: "Artist - Song [Lyrics]", Channel: "Lyrics Hub", DurationMs: 200500},
		{ID: "topic", Title: "Song", Channel: "Artist - Topic", DurationMs: 201000},
		{ID: "own", Title: "Artist - Song (Official Audio)", Channel: "Artist", DurationMs: 200000},
	}
	ranked, err := rankCandidates(candidates, Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range ranked {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "topic,own,lyrics" {
		t.Errorf("order = %v, want topic, own, lyrics", ids)
	}
}

func TestCoreTitle(t *testing.T) {
	cases := map[string]string{
		"Get Lucky (Radio Edit - feat. Pharrell Williams and Nile Rodgers)": "Get Lucky",
		"Song - Remastered 2011": "Song",
		"Song [Live]":            "Song",
		"Plain":                  "Plain",
	}
	for in, want := range cases {
		if got := coreTitle(in); got != want {
			t.Errorf("coreTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickBestMatch_RejectsWhenNothingWithinTolerance(t *testing.T) {
	candidates := []candidate{{ID: "aaa111", Title: "Totally Different Track", DurationMs: 600000}}
	if _, err := pickBestMatch(candidates, Want{Title: "Song", DurationMs: 240000}, 4000); err == nil {
		t.Fatal("expected an error when best candidate exceeds tolerance, got nil")
	}
}

func TestPickBestMatch_NoCandidates(t *testing.T) {
	if _, err := pickBestMatch(nil, Want{Title: "Song", DurationMs: 240000}, 4000); err == nil {
		t.Fatal("expected an error on empty candidates, got nil")
	}
}

// «Не нашлось» отличимо от сбоя (errors.Is), а текст ошибки прежний: он
// сохраняется в задаче и показывается админу.
func TestNoMatchErrors(t *testing.T) {
	_, err := pickBestMatch(nil, Want{Title: "Song"}, 4000)
	if !errors.Is(err, ErrNoMatch) || err.Error() != "youtube search returned no results" {
		t.Fatalf("no results: %v", err)
	}
	_, err = pickBestMatch([]candidate{{ID: "x", Title: "Other", DurationMs: 600000}}, Want{Title: "Song", DurationMs: 240000}, 4000)
	if wrapped := fmt.Errorf("match: %w", err); !errors.Is(wrapped, ErrNoMatch) {
		t.Fatalf("wrapped no match: %v", err)
	}
	if errors.Is(errors.New("yt-dlp: exit status 1"), ErrNoMatch) {
		t.Fatal("other errors are not ErrNoMatch")
	}
}

func TestVideoIDFromURL(t *testing.T) {
	ok := map[string]string{
		"dQw4w9WgXcQ": "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                "dQw4w9WgXcQ",
		"https://youtube.com/watch?feature=share&v=dQw4w9WgXcQ&t=10": "dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ&si=abc":       "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?si=xyz":                        "dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                 "dQw4w9WgXcQ",
		"  https://youtu.be/dQw4w9WgXcQ  ":                           "dQw4w9WgXcQ",
	}
	for in, want := range ok {
		if got, found := VideoIDFromURL(in); !found || got != want {
			t.Errorf("VideoIDFromURL(%q) = %q, %v", in, got, found)
		}
	}
	for _, in := range []string{"", "hello", "https://example.com/watch?v=dQw4w9WgXcQ", "https://youtube.com/watch?v=short", "dQw4w9WgXc!"} {
		if got, found := VideoIDFromURL(in); found {
			t.Errorf("VideoIDFromURL(%q) = %q, want none", in, got)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Song (Sped-Up)": "song sped up",
		"  Касания  болезненны!": "касания болезненны",
		"": "",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchQueries_FeaturedArtists(t *testing.T) {
	got := searchQueries(Want{Artist: "тихийпричал", OtherArtists: []string{"Kivirin"}, Title: "туман", Album: "туман"})
	want := []string{"тихийпричал туман", "тихийпричал Kivirin туман", "тихийпричал туман topic", "тихийпричал туман audio"}
	if !slices.Equal(got, want) {
		t.Errorf("queries = %q, want %q", got, want)
	}
	got = searchQueries(Want{Artist: "Daft Punk", Title: "One More Time", Album: "Discovery"})
	want = []string{"Daft Punk One More Time", "Daft Punk One More Time topic", "Daft Punk One More Time audio", "Daft Punk Discovery"}
	if !slices.Equal(got, want) {
		t.Errorf("queries = %q, want %q", got, want)
	}
}

// Песня того же артиста с одним общим словом в названии — не та песня, даже
// с Topic-канала и той же длины (PUNK TACTICS скачалась как LIKE A PUNK).
func TestPickBestMatch_SharedWordIsNotTheSong(t *testing.T) {
	want := Want{Artist: "Joey Valence & Brae", Title: "PUNK TACTICS", DurationMs: 143000}
	candidates := []candidate{
		{ID: "likeapunk", Title: "LIKE A PUNK", Channel: "Joey Valence & Brae - Topic", DurationMs: 143500},
		{ID: "official", Title: "Joey Valence & Brae - PUNK TACTICS (Official Video)", Channel: "Joey Valence & Brae", DurationMs: 151000},
		{ID: "reupload", Title: "Joey Valence & Brae - Punk Tactics", Channel: "WICKED PHONK", DurationMs: 144000},
	}
	got, err := pickBestMatch(candidates, want, 4000)
	if err != nil || got != "reupload" {
		t.Fatalf("got %q, %v", got, err)
	}
	// Topic с тем же названием и пометкой версии в скобках — та же песня.
	candidates = append(candidates, candidate{ID: "topic", Title: "PUNK TACTICS (feat. Someone)", Channel: "Joey Valence & Brae - Topic", DurationMs: 143000})
	if got, _ := pickBestMatch(candidates, want, 4000); got != "topic" {
		t.Fatalf("topic upload of the song itself: got %q", got)
	}
	// Topic с названием на языке оригинала — та же песня.
	jp := Want{Artist: "Hakari Hanazono", Title: "Love Love Moso Parade", DurationMs: 200000}
	if got, err := pickBestMatch([]candidate{{ID: "jp", Title: "Love Love 妄想パレード", Channel: "Hakari Hanazono (CV: Kaede Hondo) - Topic", DurationMs: 200000}}, jp, 4000); got != "jp" {
		t.Fatalf("original-script topic title: got %q, %v", got, err)
	}
}
