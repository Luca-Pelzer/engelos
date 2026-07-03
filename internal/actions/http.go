package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTP action limits. The response is capped so a huge body cannot balloon the
// engine's memory or a downstream substitution; redirects are bounded and the
// whole request is time-boxed independently of the engine's action timeout.
const (
	httpMaxResponseBytes = 256 * 1024
	httpMaxRedirects     = 5
	httpRequestTimeout   = 10 * time.Second
)

// isBlockedHost reports whether host is an SSRF target refused by default:
// "localhost"/*.localhost by name, or a literal loopback, link-local (incl. the
// 169.254.169.254 metadata endpoint), private (RFC1918/ULA) or unspecified IP.
// DNS is intentionally not resolved here, so ordinary external hostnames pass;
// the allow_private config flag opts a single request out.
func isBlockedHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return isBlockedIP(ip)
	}
	return false
}

// isBlockedIP reports whether ip falls in a loopback, link-local, private
// (RFC1918 / ULA) or unspecified range - the addresses an SSRF guard refuses.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified()
}

// HTTPDoer is the minimal client surface the http:request action needs, so a
// test can inject an httptest-backed client. *http.Client satisfies it.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// safeHTTPClient is the production client for http:request: a 15s timeout and a
// hard redirect cap. It is shared and read-only after init.
var safeHTTPClient = &http.Client{
	Timeout: httpRequestTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= httpMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", httpMaxRedirects)
		}
		return nil
	},
}

var httpAllowedMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

type httpRequestConfig struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers"`
	Body         string            `json:"body"`
	AllowPrivate bool              `json:"allow_private"`
}

// httpRequestAction performs an outbound HTTP(S) request. A nil client uses
// [safeHTTPClient]; tests inject their own.
type httpRequestAction struct {
	client HTTPDoer
}

func (httpRequestAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "http:request",
		Name:        "HTTP request",
		Description: "Sends an HTTP(S) request. Method, URL, headers and body support $(...) variables. Outputs status, body and ok.",
	}
}

func (a httpRequestAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c httpRequestConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: http:request config: %w", err)
	}

	method := strings.ToUpper(strings.TrimSpace(c.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !httpAllowedMethods[method] {
		return nil, fmt.Errorf("actions: http:request: unsupported method %q", method)
	}

	rawURL := strings.TrimSpace(c.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("actions: http:request: url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("actions: http:request: invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("actions: http:request: url scheme must be http or https, got %q", u.Scheme)
	}
	if !c.AllowPrivate && isBlockedHost(u.Hostname()) {
		return nil, fmt.Errorf("actions: http:request: refusing request to private, loopback or link-local host %q (set allow_private to override)", u.Hostname())
	}

	var body io.Reader
	if c.Body != "" {
		body = strings.NewReader(c.Body)
	}
	req, err := http.NewRequestWithContext(ec.Ctx, method, rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("actions: http:request: build request: %w", err)
	}
	for k, v := range c.Headers {
		if k = strings.TrimSpace(k); k != "" {
			req.Header.Set(k, v)
		}
	}

	client := a.client
	if client == nil {
		client = safeHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// Never surface header values or the full URL (which may carry secrets
		// in its query) in the error the engine logs; the host is enough.
		return nil, fmt.Errorf("actions: http:request %s %s: request failed", method, u.Host)
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, httpMaxResponseBytes))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300

	outputs := map[string]any{
		"status": strconv.Itoa(resp.StatusCode),
		"body":   string(data),
		"ok":     strconv.FormatBool(ok),
	}
	addJSONFieldOutputs(outputs, data)
	return &ActionResult{Outputs: outputs}, nil
}

// httpMaxJSONFields caps how many top-level fields a JSON response contributes
// as json.<key> outputs, so a large object cannot flood the output map.
const httpMaxJSONFields = 64

// addJSONFieldOutputs decodes data as a JSON object (best-effort) and copies up
// to httpMaxJSONFields top-level fields into out as json.<key>=stringified
// value, letting a later node read $(json.foo). A non-object or invalid body is
// ignored; the raw body output already covers those cases.
func addJSONFieldOutputs(out map[string]any, data []byte) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return
	}
	n := 0
	for k, v := range parsed {
		if n >= httpMaxJSONFields {
			break
		}
		out["json."+k] = stringifyValue(v)
		n++
	}
}
