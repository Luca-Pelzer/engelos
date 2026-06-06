package youtube

import (
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

// DefaultBaseURL is the production Google APIs root that hosts the YouTube Data
// API v3. Override it in tests via [WithBaseURL] to point at an httptest server.
const DefaultBaseURL = "https://www.googleapis.com"

const defaultTimeout = 10 * time.Second

// API paths, relative to the base URL.
const (
	searchPath = "/youtube/v3/search"
	videosPath = "/youtube/v3/videos"
)

// videoIDLen is the fixed length of a YouTube video ID.
const videoIDLen = 11

// Sentinel errors returned by [Client]. Compare with [errors.Is].
var (
	// ErrUnauthorized maps an HTTP 401, or an HTTP 403 whose reason is not a
	// quota problem (e.g. keyInvalid): the API key is missing or invalid.
	ErrUnauthorized = errors.New("youtube: unauthorized")
	// ErrQuotaExceeded maps an HTTP 403 whose error reason is
	// "quotaExceeded": the project's daily quota is spent.
	ErrQuotaExceeded = errors.New("youtube: quota exceeded")
	// ErrNotFound indicates the requested video does not exist:
	// [Client.GetVideo] returns it when the API responds with no items.
	ErrNotFound = errors.New("youtube: not found")
	// ErrAPI is the generic error for any other non-2xx response. It wraps
	// the API error envelope's message when present.
	ErrAPI = errors.New("youtube: api error")
)

// Video is a neutral, YouTube-wire-free view of a video.
type Video struct {
	// ID is the YouTube video ID, e.g. "dQw4w9WgXcQ".
	ID string
	// Title is the video title.
	Title string
	// Channel is the uploading channel's title (snippet.channelTitle), or ""
	// when none is present.
	Channel string
	// DurationMS is the video length in milliseconds, parsed from the
	// ISO 8601 contentDetails.duration. It is 0 for results that carry no
	// duration, such as those from [Client.Search].
	DurationMS int
}

// Client is a thin YouTube Data API v3 client. Construct it with [New]. It
// authenticates with a single API key, added as the "key" query parameter on
// every request.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
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

// New constructs a [Client] that authenticates with apiKey. With no options it
// uses [DefaultBaseURL], a 10s HTTP timeout and [slog.Default].
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// --- wire shapes (only the fields we need) ---

type wireSearchItem struct {
	ID struct {
		VideoID string `json:"videoId"`
	} `json:"id"`
	Snippet wireSnippet `json:"snippet"`
}

type wireSearch struct {
	Items []wireSearchItem `json:"items"`
}

type wireSnippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
}

type wireContentDetails struct {
	Duration string `json:"duration"`
}

type wireVideoItem struct {
	ID             string             `json:"id"`
	Snippet        wireSnippet        `json:"snippet"`
	ContentDetails wireContentDetails `json:"contentDetails"`
}

type wireVideos struct {
	Items []wireVideoItem `json:"items"`
}

type wireError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

// reasonQuotaExceeded is the error reason the API reports when the daily quota
// has been spent.
const reasonQuotaExceeded = "quotaExceeded"

// doRequest builds and sends a GET request to path (which must start with "/"),
// merging q with the API key into the query string. It returns the
// *http.Response for the caller to decode; the caller must close the body.
// Non-2xx statuses are mapped to sentinel errors here, in which case the
// response is closed and a nil *http.Response is returned alongside the error.
func (c *Client) doRequest(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("key", c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("youtube: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube: do request: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	// Map non-2xx to sentinels; consume and close the body.
	mapped := c.mapError(resp)
	_ = resp.Body.Close()
	return nil, mapped
}

// mapError converts a non-2xx response into a sentinel error. The caller still
// owns closing resp.Body.
func (c *Client) mapError(resp *http.Response) error {
	we := c.readError(resp)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		for _, e := range we.Error.Errors {
			if e.Reason == reasonQuotaExceeded {
				return ErrQuotaExceeded
			}
		}
		return ErrUnauthorized
	}

	if msg := strings.TrimSpace(we.Error.Message); msg != "" {
		return fmt.Errorf("%w: status %d: %s", ErrAPI, resp.StatusCode, msg)
	}
	return fmt.Errorf("%w: status %d", ErrAPI, resp.StatusCode)
}

// readError best-effort decodes the {"error":{...}} envelope from the body.
func (c *Client) readError(resp *http.Response) wireError {
	var we wireError
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return we
	}
	_ = json.Unmarshal(raw, &we)
	return we
}

// decodeJSON decodes resp.Body into v and closes the body.
func decodeJSON(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("youtube: decode response: %w", err)
	}
	return nil
}

// Search returns up to limit videos matching the free-text query
// (GET /youtube/v3/search?part=snippet&type=video). A non-positive limit
// defaults to 1.
//
// The search snippet carries no duration, so the returned videos have
// DurationMS == 0. Callers that need a duration should resolve the chosen ID
// with [Client.GetVideo].
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Video, error) {
	if limit <= 0 {
		limit = 1
	}
	q := url.Values{}
	q.Set("part", "snippet")
	q.Set("type", "video")
	q.Set("q", query)
	q.Set("maxResults", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, searchPath, q)
	if err != nil {
		return nil, err
	}
	var ws wireSearch
	if err := decodeJSON(resp, &ws); err != nil {
		return nil, err
	}

	videos := make([]Video, 0, len(ws.Items))
	for _, it := range ws.Items {
		videos = append(videos, Video{
			ID:      it.ID.VideoID,
			Title:   it.Snippet.Title,
			Channel: it.Snippet.ChannelTitle,
		})
	}
	return videos, nil
}

// GetVideo fetches a single video's snippet and contentDetails by ID
// (GET /youtube/v3/videos?part=snippet,contentDetails), returning a [Video]
// with its DurationMS parsed from the ISO 8601 duration. It returns
// [ErrNotFound] when the API responds with no items.
func (c *Client) GetVideo(ctx context.Context, id string) (Video, error) {
	q := url.Values{}
	q.Set("part", "snippet,contentDetails")
	q.Set("id", id)

	resp, err := c.doRequest(ctx, videosPath, q)
	if err != nil {
		return Video{}, err
	}
	var wv wireVideos
	if err := decodeJSON(resp, &wv); err != nil {
		return Video{}, err
	}
	if len(wv.Items) == 0 {
		return Video{}, ErrNotFound
	}

	it := wv.Items[0]
	durMS, err := parseISO8601Duration(it.ContentDetails.Duration)
	if err != nil {
		return Video{}, fmt.Errorf("youtube: parse duration %q: %w", it.ContentDetails.Duration, err)
	}
	return Video{
		ID:         it.ID,
		Title:      it.Snippet.Title,
		Channel:    it.Snippet.ChannelTitle,
		DurationMS: durMS,
	}, nil
}

// ParseVideoID extracts a video ID from a YouTube URL, short URL or bare ID,
// and reports whether one was found. It accepts:
//
//   - "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1s" (v query param)
//   - "https://youtu.be/dQw4w9WgXcQ" (path)
//   - "https://www.youtube.com/shorts/dQw4w9WgXcQ" (path)
//   - a bare 11-character id "dQw4w9WgXcQ"
//
// A valid id is exactly 11 characters of [A-Za-z0-9_-]. Anything else yields
// ("", false).
func ParseVideoID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}

	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")

		// youtu.be/<id>
		if host == "youtu.be" {
			return validVideoID(strings.Trim(u.Path, "/"))
		}

		// youtube.com/watch?v=<id> or youtube.com/shorts/<id>
		if host == "youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" {
			if v := u.Query().Get("v"); v != "" {
				return validVideoID(v)
			}
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i := 0; i+1 < len(parts); i++ {
				if parts[i] == "shorts" || parts[i] == "embed" || parts[i] == "v" {
					return validVideoID(parts[i+1])
				}
			}
		}
		return "", false
	}

	// Bare ID.
	return validVideoID(s)
}

// validVideoID reports whether id is exactly videoIDLen chars of
// [A-Za-z0-9_-].
func validVideoID(id string) (string, bool) {
	if len(id) != videoIDLen {
		return "", false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return "", false
		}
	}
	return id, true
}

// parseISO8601Duration parses an ISO 8601 duration of the form used by
// contentDetails.duration (e.g. "PT4M13S", "PT1H2M10S", "PT45S", "P0D") and
// returns its length in milliseconds. Only the day, hour, minute and second
// components are recognised; weeks, months and years are rejected.
func parseISO8601Duration(s string) (int, error) {
	if len(s) < 2 || s[0] != 'P' {
		return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q", s)
	}

	var totalMS int
	inTime := false
	num := ""
	sawComponent := false

	for i := 1; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == 'T':
			inTime = true
		case ch >= '0' && ch <= '9':
			num += string(ch)
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: missing number before %q", s, string(ch))
			}
			var unitMS int
			switch ch {
			case 'D':
				if inTime {
					return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: D in time section", s)
				}
				unitMS = 24 * 60 * 60 * 1000
			case 'H':
				if !inTime {
					return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: H outside time section", s)
				}
				unitMS = 60 * 60 * 1000
			case 'M':
				if !inTime {
					return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: month not supported", s)
				}
				unitMS = 60 * 1000
			case 'S':
				if !inTime {
					return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: S outside time section", s)
				}
				unitMS = 1000
			default:
				return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: unknown unit %q", s, string(ch))
			}
			totalMS += n * unitMS
			num = ""
			sawComponent = true
		}
	}

	if num != "" {
		return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: trailing number", s)
	}
	if !sawComponent {
		return 0, fmt.Errorf("youtube: invalid ISO8601 duration %q: no components", s)
	}
	return totalMS, nil
}
