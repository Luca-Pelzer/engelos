package spotify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
}

func assertBearer(t *testing.T, r *http.Request, want string) {
	t.Helper()
	assert.Equal(t, "Bearer "+want, r.Header.Get("Authorization"))
}

func TestSearch(t *testing.T) {
	const token = "tok-search"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/search", r.URL.Path)
		assertBearer(t, r, token)

		q := r.URL.Query()
		assert.Equal(t, "daft punk", q.Get("q"))
		assert.Equal(t, "track", q.Get("type"))
		assert.Equal(t, "5", q.Get("limit"))

		_, _ = io.WriteString(w, `{"tracks":{"items":[
			{"id":"4iV5W9uYEdYUVa79Axb7Rh","uri":"spotify:track:4iV5W9uYEdYUVa79Axb7Rh","name":"One More Time","duration_ms":320357,"artists":[{"name":"Daft Punk"},{"name":"Other"}]},
			{"id":"abc","uri":"spotify:track:abc","name":"Harder","duration_ms":100,"artists":[]}
		]}}`)
	}))

	got, err := c.Search(context.Background(), token, "daft punk", 5)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, Track{
		ID:         "4iV5W9uYEdYUVa79Axb7Rh",
		URI:        "spotify:track:4iV5W9uYEdYUVa79Axb7Rh",
		Name:       "One More Time",
		Artist:     "Daft Punk",
		DurationMS: 320357,
	}, got[0])
	assert.Equal(t, "", got[1].Artist, "no artists -> empty artist")
}

func TestSearchDefaultsLimit(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("limit"))
		_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
	}))
	got, err := c.Search(context.Background(), "t", "x", 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetTrack(t *testing.T) {
	const token = "tok-get"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/tracks/4iV5W9uYEdYUVa79Axb7Rh", r.URL.Path)
		assertBearer(t, r, token)
		_, _ = io.WriteString(w, `{"id":"4iV5W9uYEdYUVa79Axb7Rh","uri":"spotify:track:4iV5W9uYEdYUVa79Axb7Rh","name":"Around the World","duration_ms":429000,"artists":[{"name":"Daft Punk"}]}`)
	}))

	tr, err := c.GetTrack(context.Background(), token, "4iV5W9uYEdYUVa79Axb7Rh")
	require.NoError(t, err)
	assert.Equal(t, "Around the World", tr.Name)
	assert.Equal(t, "Daft Punk", tr.Artist)
	assert.Equal(t, 429000, tr.DurationMS)
	assert.Equal(t, "spotify:track:4iV5W9uYEdYUVa79Axb7Rh", tr.URI)
}

func TestAddToPlaylist(t *testing.T) {
	const token = "tok-add"
	const uri = "spotify:track:4iV5W9uYEdYUVa79Axb7Rh"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/playlists/PL123/tracks", r.URL.Path)
		assertBearer(t, r, token)

		var body struct {
			URIs []string `json:"uris"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, []string{uri}, body.URIs)

		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"snapshot_id":"snap-1"}`)
	}))

	require.NoError(t, c.AddToPlaylist(context.Background(), token, "PL123", uri))
}

func TestPlaylistTracks(t *testing.T) {
	const token = "tok-pl"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/playlists/PL123/tracks", r.URL.Path)
		assertBearer(t, r, token)
		_, _ = io.WriteString(w, `{"items":[
			{"track":{"id":"id1","uri":"spotify:track:id1","name":"First","duration_ms":1000,"artists":[{"name":"A"}]}},
			{"track":{"id":"id2","uri":"spotify:track:id2","name":"Second","duration_ms":2000,"artists":[{"name":"B"}]}}
		]}`)
	}))

	got, err := c.PlaylistTracks(context.Background(), token, "PL123")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "First", got[0].Name)
	assert.Equal(t, "B", got[1].Artist)
}

func TestRemoveFromPlaylist(t *testing.T) {
	const token = "tok-rm"
	const uri = "spotify:track:id1"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/playlists/PL123/tracks", r.URL.Path)
		assertBearer(t, r, token)

		var body struct {
			Tracks []struct {
				URI string `json:"uri"`
			} `json:"tracks"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Len(t, body.Tracks, 1)
		assert.Equal(t, uri, body.Tracks[0].URI)

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"snapshot_id":"snap-2"}`)
	}))

	require.NoError(t, c.RemoveFromPlaylist(context.Background(), token, "PL123", uri))
}

func TestNowPlayingPlaying(t *testing.T) {
	const token = "tok-np"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/me/player/currently-playing", r.URL.Path)
		assertBearer(t, r, token)
		_, _ = io.WriteString(w, `{"is_playing":true,"item":{"id":"id9","uri":"spotify:track:id9","name":"Live","duration_ms":3000,"artists":[{"name":"Z"}]}}`)
	}))

	tr, ok, err := c.NowPlaying(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "Live", tr.Name)
	assert.Equal(t, "Z", tr.Artist)
}

func TestNowPlayingNoContent(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tr, ok, err := c.NowPlaying(context.Background(), "tok")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, Track{}, tr)
}

func TestNowPlayingNullItem(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"is_playing":false,"item":null}`)
	}))
	tr, ok, err := c.NowPlaying(context.Background(), "tok")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, Track{}, tr)
}

func TestSkip(t *testing.T) {
	const token = "tok-skip"
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/me/player/next", r.URL.Path)
		assertBearer(t, r, token)
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, c.Skip(context.Background(), token))
}

func TestStatusMappingUnauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"status":401,"message":"The access token expired"}}`)
	}))
	_, err := c.Search(context.Background(), "tok", "x", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestStatusMappingPremiumRequired(t *testing.T) {
	// 403 on a player endpoint -> ErrPremiumRequired.
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/me/player/next", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"status":403,"message":"Player command failed: Premium required"}}`)
	}))
	err := c.Skip(context.Background(), "tok")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPremiumRequired)
}

func TestStatusMappingNoActiveDevice(t *testing.T) {
	// 404 on /me/player/next -> ErrNoActiveDevice.
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/me/player/next", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"status":404,"message":"No active device found"}}`)
	}))
	err := c.Skip(context.Background(), "tok")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoActiveDevice)
}

func TestStatusMappingForbiddenNonPlayerIsAPI(t *testing.T) {
	// 403 on a non-player endpoint is a generic API error, NOT premium.
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"status":403,"message":"Forbidden"}}`)
	}))
	_, err := c.GetTrack(context.Background(), "tok", "4iV5W9uYEdYUVa79Axb7Rh")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAPI)
	assert.NotErrorIs(t, err, ErrPremiumRequired)
}

func TestStatusMappingGenericAPIError(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"status":500,"message":"boom"}}`)
	}))
	_, err := c.Search(context.Background(), "tok", "x", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "boom")
}

func TestParseTrackID(t *testing.T) {
	const want = "4iV5W9uYEdYUVa79Axb7Rh"
	tests := []struct {
		name   string
		in     string
		wantID string
		wantOK bool
	}{
		{"open url with query", "https://open.spotify.com/track/4iV5W9uYEdYUVa79Axb7Rh?si=abc123", want, true},
		{"open url no query", "https://open.spotify.com/track/4iV5W9uYEdYUVa79Axb7Rh", want, true},
		{"uri", "spotify:track:4iV5W9uYEdYUVa79Axb7Rh", want, true},
		{"bare id", "4iV5W9uYEdYUVa79Axb7Rh", want, true},
		{"bare id whitespace", "  4iV5W9uYEdYUVa79Axb7Rh  ", want, true},
		{"junk", "not a track", "", false},
		{"empty", "", "", false},
		{"wrong length bare", "tooshort", "", false},
		{"uri wrong length", "spotify:track:short", "", false},
		{"album url", "https://open.spotify.com/album/4iV5W9uYEdYUVa79Axb7Rh", "", false},
		{"id with bad char", "4iV5W9uYEdYUVa79Axb7R!", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ParseTrackID(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}
