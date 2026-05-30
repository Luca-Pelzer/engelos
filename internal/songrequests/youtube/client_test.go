package youtube

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAPIKey = "test-api-key"

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(
		testAPIKey,
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
}

func TestSearch(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/youtube/v3/search", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "snippet", q.Get("part"))
		assert.Equal(t, "video", q.Get("type"))
		assert.Equal(t, "rick astley", q.Get("q"))
		assert.Equal(t, "5", q.Get("maxResults"))
		assert.Equal(t, testAPIKey, q.Get("key"))

		_, _ = io.WriteString(w, `{"items":[
			{"id":{"videoId":"dQw4w9WgXcQ"},"snippet":{"title":"Never Gonna Give You Up","channelTitle":"Rick Astley"}},
			{"id":{"videoId":"abcdefghijk"},"snippet":{"title":"Other","channelTitle":""}}
		]}`)
	}))

	got, err := c.Search(context.Background(), "rick astley", 5)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, Video{
		ID:      "dQw4w9WgXcQ",
		Title:   "Never Gonna Give You Up",
		Channel: "Rick Astley",
	}, got[0])
	assert.Equal(t, 0, got[0].DurationMS, "search snippet has no duration")
	assert.Equal(t, "", got[1].Channel, "empty channelTitle -> empty Channel")
}

func TestSearchDefaultsLimit(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("maxResults"))
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	got, err := c.Search(context.Background(), "x", 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetVideo(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/youtube/v3/videos", r.URL.Path)

		q := r.URL.Query()
		assert.Equal(t, "snippet,contentDetails", q.Get("part"))
		assert.Equal(t, "dQw4w9WgXcQ", q.Get("id"))
		assert.Equal(t, testAPIKey, q.Get("key"))

		_, _ = io.WriteString(w, `{"items":[
			{"id":"dQw4w9WgXcQ","snippet":{"title":"Never Gonna Give You Up","channelTitle":"Rick Astley"},"contentDetails":{"duration":"PT3M33S"}}
		]}`)
	}))

	v, err := c.GetVideo(context.Background(), "dQw4w9WgXcQ")
	require.NoError(t, err)
	assert.Equal(t, "dQw4w9WgXcQ", v.ID)
	assert.Equal(t, "Never Gonna Give You Up", v.Title)
	assert.Equal(t, "Rick Astley", v.Channel)
	assert.Equal(t, 213000, v.DurationMS, "PT3M33S -> 213000ms")
}

func TestGetVideoEmptyIsNotFound(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	_, err := c.GetVideo(context.Background(), "dQw4w9WgXcQ")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStatusMappingQuotaExceeded(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":403,"message":"The request cannot be completed because you have exceeded your quota.","errors":[{"reason":"quotaExceeded"}]}}`)
	}))
	_, err := c.Search(context.Background(), "x", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestStatusMappingForbiddenNonQuotaIsUnauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":403,"message":"The request is missing a valid API key.","errors":[{"reason":"keyInvalid"}]}}`)
	}))
	_, err := c.Search(context.Background(), "x", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.NotErrorIs(t, err, ErrQuotaExceeded)
}

func TestStatusMappingUnauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"message":"Invalid Credentials","errors":[{"reason":"authError"}]}}`)
	}))
	_, err := c.GetVideo(context.Background(), "dQw4w9WgXcQ")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestStatusMappingGenericAPIError(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":500,"message":"backend boom"}}`)
	}))
	_, err := c.Search(context.Background(), "x", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "backend boom")
}

func TestParseVideoID(t *testing.T) {
	const want = "dQw4w9WgXcQ"
	tests := []struct {
		name   string
		in     string
		wantID string
		wantOK bool
	}{
		{"watch url with extra params", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1s", want, true},
		{"watch url no www", "https://youtube.com/watch?v=dQw4w9WgXcQ", want, true},
		{"youtu.be short", "https://youtu.be/dQw4w9WgXcQ", want, true},
		{"youtu.be short with query", "https://youtu.be/dQw4w9WgXcQ?t=30", want, true},
		{"shorts url", "https://www.youtube.com/shorts/dQw4w9WgXcQ", want, true},
		{"embed url", "https://www.youtube.com/embed/dQw4w9WgXcQ", want, true},
		{"music subdomain", "https://music.youtube.com/watch?v=dQw4w9WgXcQ", want, true},
		{"bare id", "dQw4w9WgXcQ", want, true},
		{"bare id whitespace", "  dQw4w9WgXcQ  ", want, true},
		{"id with dash underscore", "a_b-c1D2e3F", "a_b-c1D2e3F", true},
		{"junk", "not a video", "", false},
		{"empty", "", "", false},
		{"too short bare", "short", "", false},
		{"too long bare", "dQw4w9WgXcQXX", "", false},
		{"watch url short id", "https://www.youtube.com/watch?v=short", "", false},
		{"non-youtube host", "https://vimeo.com/dQw4w9WgXcQ", "", false},
		{"id with bad char", "dQw4w9WgXc!", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ParseVideoID(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantMS  int
		wantErr bool
	}{
		{"minutes seconds", "PT4M13S", 253000, false},
		{"one hour", "PT1H", 3600000, false},
		{"seconds only", "PT45S", 45000, false},
		{"hour minute second", "PT1H2M10S", 3730000, false},
		{"zero days", "P0D", 0, false},
		{"hours minutes only", "PT2H30M", 9000000, false},
		{"large seconds", "PT3600S", 3600000, false},
		{"empty", "", 0, true},
		{"no P prefix", "T4M13S", 0, true},
		{"garbage", "PTXYZ", 0, true},
		{"trailing number", "PT4M13", 0, true},
		{"no components", "P", 0, true},
		{"S outside time", "P45S", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseISO8601Duration(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantMS, got)
		})
	}
}
