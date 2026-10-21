package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

type SimilarArtistsResponse struct {
	Artist           string `json:"artist"`
	ArtistID         string `json:"artist_id"`
	ComponentMatches []struct {
		Artist1RepresentativeSongs []struct {
			ItemID string `json:"item_id"`
			Title  string `json:"title"`
		} `json:"artist1_representative_songs"`
		Artist2RepresentativeSongs []struct {
			ItemID string `json:"item_id"`
			Title  string `json:"title"`
		} `json:"artist2_representative_songs"`
	} `json:"component_matches"`
}

// creditSeparator splits a track-artist credit into its artists.
var creditSeparator = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring)\s+|\s*[,&•]\s*`)

// primaryCredit returns the first artist of a credit: "A feat. B" -> "A".
func primaryCredit(credit string) string {
	return strings.TrimSpace(creditSeparator.Split(credit, 2)[0])
}

func getSimilarArtists(id string, includeComponentMatches bool) ([]SimilarArtistsResponse, error) {
	return withServers(func(server string) ([]SimilarArtistsResponse, error) {
		return getSimilarArtistsFrom(server, id, includeComponentMatches)
	})
}

func getSimilarArtistsFrom(server, id string, includeComponentMatches bool) ([]SimilarArtistsResponse, error) {
	apiBaseURL := getConfigString(configAPIUrl, defaultAPIUrl)
	artistCount := getConfigInt("artistSimilarCount", defaultArtistSimilarCount)

	params := url.Values{}
	// artist_id is resolved on `server`; `artist` only on AudioMuse's default server.
	params.Set("artist_id", id)
	params.Set("n", strconv.Itoa(artistCount))
	params.Set("include_component_matches", strconv.FormatBool(includeComponentMatches))
	if server != "" {
		params.Set("server", server)
	}

	apiURL := fmt.Sprintf("%s/api/similar_artists?%s", apiBaseURL, params.Encode())
	pdk.Log(pdk.LogInfo, fmt.Sprintf("[AudioMuse] Calling GetSimilarArtists API for artist ID %s: %s", id, apiURL))

	// Make HTTP GET request to AudioMuse-AI using the host HTTP service.
	// This uses host.HTTPSend as recommended by Navidrome upstream (migrated from pdk.NewHTTPRequest).
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:  "GET",
		URL:     apiURL,
		Headers: authHeaders(),
	})
	if err != nil {
		errMsg := fmt.Sprintf("[AudioMuse] ERROR: HTTP request failed: %v", err)
		pdk.Log(pdk.LogError, errMsg)
		return nil, fmt.Errorf("AudioMuse-AI HTTP request failed: %w", err)
	}

	if resp == nil {
		errMsg := "[AudioMuse] ERROR: empty HTTP response"
		pdk.Log(pdk.LogError, errMsg)
		return nil, fmt.Errorf("AudioMuse-AI returned an empty HTTP response")
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("[AudioMuse] API response status: %d", resp.StatusCode))
	if resp.StatusCode != 200 {
		errMsg := fmt.Sprintf("[AudioMuse] ERROR: AudioMuse-AI returned status %d", resp.StatusCode)
		pdk.Log(statusLogLevel(resp.StatusCode), errMsg)
		return nil, statusError(resp.StatusCode, "")
	}

	var artists []SimilarArtistsResponse

	body := resp.Body
	if err := json.Unmarshal(body, &artists); err != nil {
		errMsg := fmt.Sprintf("[AudioMuse] ERROR: Failed to parse artist response: %v", err)
		pdk.Log(pdk.LogError, errMsg)
		return nil, fmt.Errorf("failed to parse AudioMuse-AI artist response: %w", err)
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("[AudioMuse] Successfully parsed %d similar artists", len(artists)))

	return artists, nil
}
