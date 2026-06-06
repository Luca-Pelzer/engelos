package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production Spotify Web API root. Override it in tests
// via [WithBaseURL] to point at an httptest server.
const DefaultBaseURL = "https://api.spotify.com"

const defaultTimeout = 10 * time.Second

// Sentinel errors returned by [Client]. Compare with [errors.Is].
var (
	// ErrPremiumRequired maps an HTTP 403 from a player endpoint: the
	// account is not Spotify Premium, so playback control is unavailable.
	ErrPremiumRequired = errors.New("spotify: premium required")
	// ErrNoActiveDevice maps an HTTP 404 from a player endpoint: there is
	// no active Spotify device to control.
	ErrNoActiveDevice = errors.New("spotify: no active device")
	// ErrUnauthorized maps an HTTP 401: the access token is missing,
	// expired or invalid. The caller should refresh the token and retry.
	ErrUnauthorized = errors.New("spotify: unauthorized")
	// ErrNotPlaying indicates nothing is currently playing. [Client.NowPlaying]
	// reports this state via ok=false rather than returning this error; it is
	// provided for callers that prefer an explicit sentinel.
	ErrNotPlaying = errors.New("spotify: nothing playing")
	// ErrAPI is the generic error for any other non-2xx response. It wraps
	// the Spotify error envelope's message when present.
	ErrAPI = errors.New("spotify: api error")
)

// Track is a neutral, Spotify-wire-free view of a track.
type Track struct {
	// ID is the Spotify track ID, e.g. "4iV5W9uYEdYUVa79Axb7Rh".
	ID string
	// URI is the Spotify track URI, e.g. "spotify:track:4iV5W9uYEdYUVa79Axb7Rh".
	URI string
	// Name is the track title.
	Name string
	// Artist is the first artist's name, or "" when none is present.
	Artist string
	// DurationMS is the track length in milliseconds.
	DurationMS int
}

// Client is a thin Spotify Web API client. Construct it with [New]. Every
// method takes a per-call bearer token; the client holds no credentials.
type Client struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

// Option configures a [Client] in [New].
type Option func(*Client)

// WithHTTPClient sets the underlying *http.Client. A nil client is ignored.
// The default is &http.Client{Timeout: 10 * time.Second}.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithBaseURL overrides the API root (default [DefaultBaseURL]). It is used by
// tests to target an httptest server. A trailing slash is trimmed. An empty
// value is ignored.
func WithBaseURL(base string) Option {
	return func(c *Client) {
		if base != "" {
			c.baseURL = strings.TrimRight(base, "/")
		}
	}
}

// WithLogger sets the structured logger. A nil logger is ignored; the default
// is [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// New constructs a [Client]. With no options it uses [DefaultBaseURL], a 10s
// HTTP timeout and [slog.Default].
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// --- wire shapes (only the fields we need) ---

type wireArtist struct {
	Name string `json:"name"`
}

type wireTrack struct {
	ID         string       `json:"id"`
	URI        string       `json:"uri"`
	Name       string       `json:"name"`
	DurationMS int          `json:"duration_ms"`
	Artists    []wireArtist `json:"artists"`
}

type wireSearch struct {
	Tracks struct {
		Items []wireTrack `json:"items"`
	} `json:"tracks"`
}

type wirePlaylistTracks struct {
	Items []struct {
		Track wireTrack `json:"track"`
	} `json:"items"`
}

type wireNowPlaying struct {
	Item      *wireTrack `json:"item"`
	IsPlaying bool       `json:"is_playing"`
}

type wireError struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// toTrack converts a Spotify wire track into the neutral [Track]. The artist is
// the first entry of artists, or "" when the list is empty.
func toTrack(w wireTrack) Track {
	artist := ""
	if len(w.Artists) > 0 {
		artist = w.Artists[0].Name
	}
	return Track{
		ID:         w.ID,
		URI:        w.URI,
		Name:       w.Name,
		Artist:     artist,
		DurationMS: w.DurationMS,
	}
}

// playerEndpoint reports whether path targets a /me/player endpoint, for which
// 403 and 404 carry the device/premium-specific meanings.
func playerEndpoint(path string) bool {
	return strings.HasPrefix(path, "/v1/me/player")
}

// doRequest builds and sends a request to path (which must start with "/"),
// setting the bearer token and an optional JSON body. It returns the *http.Response
// for the caller to decode; the caller must close the body. Non-2xx statuses
// are mapped to sentinel errors here, in which case the response is closed and
// a nil *http.Response is returned alongside the error.
func (c *Client) doRequest(ctx context.Context, method, path, token string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("spotify: marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("spotify: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spotify: do request: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	// Map non-2xx to sentinels; consume and close the body.
	mapped := c.mapError(resp, path)
	_ = resp.Body.Close()
	return nil, mapped
}

// mapError converts a non-2xx response into a sentinel error. The caller still
// owns closing resp.Body.
func (c *Client) mapError(resp *http.Response, path string) error {
	player := playerEndpoint(path)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		if player {
			return ErrPremiumRequired
		}
	case http.StatusNotFound:
		if player {
			return ErrNoActiveDevice
		}
	}

	// Generic: try to surface the API's error message.
	msg := c.readErrorMessage(resp)
	if msg != "" {
		return fmt.Errorf("%w: status %d: %s", ErrAPI, resp.StatusCode, msg)
	}
	return fmt.Errorf("%w: status %d", ErrAPI, resp.StatusCode)
}

// readErrorMessage best-effort extracts {"error":{"message":...}} from the body.
func (c *Client) readErrorMessage(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var we wireError
	if json.Unmarshal(raw, &we) == nil && we.Error.Message != "" {
		return we.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

// decodeJSON decodes resp.Body into v and closes the body.
func decodeJSON(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("spotify: decode response: %w", err)
	}
	return nil
}

// Search returns up to limit tracks matching the free-text query
// (GET /v1/search?type=track). A non-positive limit defaults to 1.
func (c *Client) Search(ctx context.Context, token, query string, limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 1
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("type", "track")
	q.Set("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/search?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	var ws wireSearch
	if err := decodeJSON(resp, &ws); err != nil {
		return nil, err
	}

	tracks := make([]Track, 0, len(ws.Tracks.Items))
	for _, it := range ws.Tracks.Items {
		tracks = append(tracks, toTrack(it))
	}
	return tracks, nil
}

// GetTrack looks up a single track by ID (GET /v1/tracks/{id}).
func (c *Client) GetTrack(ctx context.Context, token, id string) (Track, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/tracks/"+url.PathEscape(id), token, nil)
	if err != nil {
		return Track{}, err
	}
	var wt wireTrack
	if err := decodeJSON(resp, &wt); err != nil {
		return Track{}, err
	}
	return toTrack(wt), nil
}

// AddToPlaylist appends a single track to a playlist
// (POST /v1/playlists/{playlistID}/tracks with body {"uris":[trackURI]}).
func (c *Client) AddToPlaylist(ctx context.Context, token, playlistID, trackURI string) error {
	body := map[string]any{"uris": []string{trackURI}}
	resp, err := c.doRequest(ctx, http.MethodPost,
		"/v1/playlists/"+url.PathEscape(playlistID)+"/tracks", token, body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// PlaylistTracks lists the tracks currently in a playlist
// (GET /v1/playlists/{playlistID}/tracks).
func (c *Client) PlaylistTracks(ctx context.Context, token, playlistID string) ([]Track, error) {
	resp, err := c.doRequest(ctx, http.MethodGet,
		"/v1/playlists/"+url.PathEscape(playlistID)+"/tracks", token, nil)
	if err != nil {
		return nil, err
	}
	var wp wirePlaylistTracks
	if err := decodeJSON(resp, &wp); err != nil {
		return nil, err
	}

	tracks := make([]Track, 0, len(wp.Items))
	for _, it := range wp.Items {
		tracks = append(tracks, toTrack(it.Track))
	}
	return tracks, nil
}

// RemoveFromPlaylist removes all occurrences of a track from a playlist
// (DELETE /v1/playlists/{playlistID}/tracks with body
// {"tracks":[{"uri":trackURI}]}).
func (c *Client) RemoveFromPlaylist(ctx context.Context, token, playlistID, trackURI string) error {
	body := map[string]any{
		"tracks": []map[string]string{{"uri": trackURI}},
	}
	resp, err := c.doRequest(ctx, http.MethodDelete,
		"/v1/playlists/"+url.PathEscape(playlistID)+"/tracks", token, body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// NowPlaying returns the currently playing track. When nothing is playing -
// including an HTTP 204 No Content - it returns (Track{}, false, nil).
// (GET /v1/me/player/currently-playing).
func (c *Client) NowPlaying(ctx context.Context, token string) (Track, bool, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/me/player/currently-playing", token, nil)
	if err != nil {
		return Track{}, false, err
	}
	if resp.StatusCode == http.StatusNoContent {
		_ = resp.Body.Close()
		return Track{}, false, nil
	}
	var wn wireNowPlaying
	if err := decodeJSON(resp, &wn); err != nil {
		return Track{}, false, err
	}
	if wn.Item == nil {
		return Track{}, false, nil
	}
	return toTrack(*wn.Item), wn.IsPlaying, nil
}

// Skip advances playback to the next track (POST /v1/me/player/next).
func (c *Client) Skip(ctx context.Context, token string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/v1/me/player/next", token, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// trackIDLen is the fixed length of a Spotify base62 track ID.
const trackIDLen = 22

// ParseTrackID extracts a track ID from a Spotify URL, URI or bare ID, and
// reports whether one was found. It accepts:
//
//   - "https://open.spotify.com/track/4iV5W9uYEdYUVa79Axb7Rh?si=..." (query stripped)
//   - "spotify:track:4iV5W9uYEdYUVa79Axb7Rh"
//   - a bare 22-character base62 id "4iV5W9uYEdYUVa79Axb7Rh"
//
// Anything else yields ("", false).
func ParseTrackID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}

	// URI form: spotify:track:<id>
	if strings.HasPrefix(s, "spotify:track:") {
		id := strings.TrimPrefix(s, "spotify:track:")
		return validBase62ID(id)
	}

	// URL form: https://open.spotify.com/track/<id>?...
	if strings.Contains(s, "open.spotify.com") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		// Path like /track/<id> (query already separated by url.Parse).
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] == "track" {
				return validBase62ID(parts[i+1])
			}
		}
		return "", false
	}

	// Bare ID.
	return validBase62ID(s)
}

// validBase62ID reports whether id is exactly trackIDLen base62 chars.
func validBase62ID(id string) (string, bool) {
	if len(id) != trackIDLen {
		return "", false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return "", false
		}
	}
	return id, true
}
