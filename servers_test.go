package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"audiomuse-navidrome-plugin/sonicsimilarity"
	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/stretchr/testify/mock"
)

const notFoundBody = `{"error":"Target track not found in index or no similar tracks found."}`

// setup wires the PDK mocks: config from cfg (missing keys unset), logs ignored.
func setup(t *testing.T, cfg map[string]string) {
	t.Helper()
	pdk.ResetMock()
	host.HTTPMock.ExpectedCalls = nil
	host.HTTPMock.Calls = nil
	pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Return()
	for _, key := range []string{configAPIUrl, configAPIToken, configServer, configEliminateDuplicates,
		configRadiusSimilarity, configInstantMixSource, "artistSimilarCount"} {
		v, ok := cfg[key]
		pdk.PDKMock.On("GetConfig", key).Return(v, ok)
	}
}

// serverOf returns the server an outbound request targets, from its query or JSON body.
func serverOf(req host.HTTPRequest) string {
	if len(req.Body) > 0 {
		var body map[string]any
		_ = json.Unmarshal(req.Body, &body)
		s, _ := body["server"].(string)
		return s
	}
	u, _ := url.Parse(req.URL)
	return u.Query().Get("server")
}

// onServer answers requests for server with the given status and body.
func onServer(server string, status int32, body string) {
	host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
		return serverOf(req) == server
	})).Return(&host.HTTPResponse{StatusCode: status, Body: []byte(body)}, nil)
}

func TestSimilarSongsFallsThroughOn404(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id, lera-id"})
	onServer("tom-id", 404, notFoundBody)
	onServer("lera-id", 200, `[{"item_id":"l2","title":"Lera song","author":"A","album":"B","distance":0.1}]`)

	resp, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "l1", Count: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Songs) != 1 || resp.Songs[0].ID != "l2" {
		t.Fatalf("want Lera's song, got %+v", resp.Songs)
	}
	host.HTTPMock.AssertNumberOfCalls(t, "Send", 2)
}

func TestFirstServerHitStops(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id"})
	onServer("tom-id", 200, `[{"item_id":"t2","title":"Tom song"}]`)

	resp, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "t1", Count: 5})
	if err != nil || len(resp.Songs) != 1 || resp.Songs[0].ID != "t2" {
		t.Fatalf("want Tom's song, got %+v, %v", resp, err)
	}
	host.HTTPMock.AssertNumberOfCalls(t, "Send", 1)
}

func TestAllServers404ReturnsError(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id"})
	onServer("tom-id", 404, notFoundBody)
	onServer("lera-id", 404, notFoundBody)

	if _, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "x", Count: 5}); err == nil {
		t.Fatal("want error when no server knows the track")
	}
	host.HTTPMock.AssertNumberOfCalls(t, "Send", 2)
}

func TestNon404ErrorDoesNotFallThrough(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id"})
	onServer("tom-id", 500, `{"error":"boom"}`)
	onServer("lera-id", 200, `[{"item_id":"l2"}]`)

	if _, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "x", Count: 5}); err == nil {
		t.Fatal("want the 500 surfaced, not masked by the next server")
	}
	host.HTTPMock.AssertNumberOfCalls(t, "Send", 1)
}

func TestBlankServerSendsNoParam(t *testing.T) {
	setup(t, map[string]string{})
	host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
		return !strings.Contains(req.URL, "server=")
	})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"item_id":"d2"}]`)}, nil)

	resp, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "d1", Count: 5})
	if err != nil || len(resp.Songs) != 1 {
		t.Fatalf("got %+v, %v", resp, err)
	}
}

func TestLyricsSourceFallsThrough(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id", configInstantMixSource: instantMixLyricsBySong})
	onServer("tom-id", 404, notFoundBody)
	onServer("lera-id", 200, `{"results":[{"item_id":"l1","is_seed":true},{"item_id":"l2","similarity":0.8}]}`)

	resp, err := (&audioMusePlugin{}).GetSimilarSongsByTrack(metadata.SimilarSongsByTrackRequest{ID: "l1", Count: 5})
	if err != nil || len(resp.Songs) != 1 || resp.Songs[0].ID != "l2" {
		t.Fatalf("got %+v, %v", resp, err)
	}
}

func TestSimilarArtistsFallsThrough(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id"})
	onServer("tom-id", 404, notFoundBody)
	onServer("lera-id", 200, `[{"artist":"X","artist_id":"ax"}]`)

	resp, err := (&audioMusePlugin{}).GetSimilarArtists(metadata.SimilarArtistsRequest{ID: "a1"})
	if err != nil || len(resp.Artists) != 1 || resp.Artists[0].ID != "ax" {
		t.Fatalf("got %+v, %v", resp, err)
	}
}

// AudioMuse resolves `artist` ids on the default server only; `artist_id` uses `server`.
func TestSimilarArtistsSendsArtistID(t *testing.T) {
	setup(t, map[string]string{configServer: "lera-id"})
	host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
		return strings.Contains(req.URL, "artist_id=a1") && !strings.Contains(req.URL, "artist=a1")
	})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[{"artist":"X","artist_id":"ax"}]`)}, nil)

	resp, err := (&audioMusePlugin{}).GetSimilarArtists(metadata.SimilarArtistsRequest{ID: "a1"})
	if err != nil || len(resp.Artists) != 1 {
		t.Fatalf("got %+v, %v", resp, err)
	}
}

// AudioMuse indexes track-artist credits ("A feat. B") that have no Navidrome artist id;
// send the primary credit by name so Navidrome's name match can still link it.
func TestSimilarArtistsWithoutIDUsePrimaryCredit(t *testing.T) {
	setup(t, map[string]string{})
	host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(`[
		{"artist":"NILETTO feat. MaxWell Show","artist_id":null},
		{"artist":"Madonna","artist_id":"md"},
		{"artist":"Seed","artist_id":"a1"},
		{"artist":"Seed ft. Someone","artist_id":null},
		{"artist":"niletto, DJ M.E.G.","artist_id":null},
		{"artist":"Morandi & Eneli","artist_id":null},
		{"artist":"Madonna • Kimi Djabatè","artist_id":null},
		{"artist":"TERIYAKI BOYZ feat. Pusha T","artist_id":"ax"}]`)}, nil)

	resp, err := (&audioMusePlugin{}).GetSimilarArtists(metadata.SimilarArtistsRequest{ID: "a1", Name: "Seed"})
	if err != nil {
		t.Fatal(err)
	}
	want := []metadata.ArtistRef{{Name: "NILETTO"}, {ID: "md", Name: "Madonna"}, {Name: "Morandi"}, {ID: "ax", Name: "TERIYAKI BOYZ feat. Pusha T"}}
	if fmt.Sprint(resp.Artists) != fmt.Sprint(want) {
		t.Fatalf("got %+v, want %+v", resp.Artists, want)
	}
}

func TestSonicPathFallsThrough(t *testing.T) {
	setup(t, map[string]string{configServer: "tom-id,lera-id"})
	onServer("tom-id", 404, `{"error":"No path found"}`)
	onServer("lera-id", 200, `{"path":[{"item_id":"p1"},{"item_id":"p2"}]}`)

	resp, err := (&audioMusePlugin{}).FindSonicPath(sonicsimilarity.FindSonicPathRequest{
		StartSong: metadata.SongRef{ID: "s"}, EndSong: metadata.SongRef{ID: "e"},
	})
	if err != nil || len(resp.Matches) != 2 {
		t.Fatalf("got %+v, %v", resp, err)
	}
}
