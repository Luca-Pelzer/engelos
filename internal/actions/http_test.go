package actions

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDoer records the last request and returns a canned response or error.
type fakeDoer struct {
	lastReq *http.Request
	resp    *http.Response
	err     error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func newResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestHTTPRequest_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Action("http:request")
	assert.True(t, ok)
}

func TestHTTPRequest_SuccessOutputs(t *testing.T) {
	doer := &fakeDoer{resp: newResp(200, `{"ok":true}`)}
	act := httpRequestAction{client: doer}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"method":  "POST",
		"url":     "https://example.test/api",
		"headers": map[string]string{"X-Test": "secret-value"},
		"body":    "hello",
	}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "200", res.Outputs["status"])
	assert.Equal(t, `{"ok":true}`, res.Outputs["body"])
	assert.Equal(t, "true", res.Outputs["ok"])

	require.NotNil(t, doer.lastReq)
	assert.Equal(t, http.MethodPost, doer.lastReq.Method)
	assert.Equal(t, "secret-value", doer.lastReq.Header.Get("X-Test"))
	sent, _ := io.ReadAll(doer.lastReq.Body)
	assert.Equal(t, "hello", string(sent))
}

func TestHTTPRequest_NonSuccessOkFalse(t *testing.T) {
	doer := &fakeDoer{resp: newResp(503, "down")}
	act := httpRequestAction{client: doer}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"url": "https://example.test"}))
	require.NoError(t, err)
	assert.Equal(t, "503", res.Outputs["status"])
	assert.Equal(t, "false", res.Outputs["ok"])
}

func TestHTTPRequest_DefaultsToGET(t *testing.T) {
	doer := &fakeDoer{resp: newResp(200, "")}
	act := httpRequestAction{client: doer}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"url": "https://example.test"}))
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, doer.lastReq.Method)
}

func TestHTTPRequest_RejectsUnsupportedMethod(t *testing.T) {
	act := httpRequestAction{client: &fakeDoer{resp: newResp(200, "")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"method": "CONNECT", "url": "https://example.test"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported method")
}

func TestHTTPRequest_RejectsNonHTTPScheme(t *testing.T) {
	act := httpRequestAction{client: &fakeDoer{resp: newResp(200, "")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"url": "file:///etc/passwd"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestHTTPRequest_RequiresURL(t *testing.T) {
	act := httpRequestAction{client: &fakeDoer{resp: newResp(200, "")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"url": "  "}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

func TestHTTPRequest_ErrorHidesURLAndHeaders(t *testing.T) {
	doer := &fakeDoer{err: errors.New("dial tcp: connection refused to 10.0.0.5")}
	act := httpRequestAction{client: doer}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{
		"url":     "https://secret.host/path?token=abc123",
		"headers": map[string]string{"Authorization": "Bearer top-secret"},
	}))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "token=abc123")
	assert.NotContains(t, err.Error(), "top-secret")
	assert.Contains(t, err.Error(), "secret.host")
}

func TestHTTPRequest_CapsResponseBody(t *testing.T) {
	big := strings.Repeat("a", httpMaxResponseBytes+1024)
	doer := &fakeDoer{resp: newResp(200, big)}
	act := httpRequestAction{client: doer}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"url": "https://example.test"}))
	require.NoError(t, err)
	assert.Len(t, res.Outputs["body"], httpMaxResponseBytes)
}

// TestHTTPRequest_SSRFGuardRejectsPrivateTargets asserts the default-deny guard
// refuses loopback, link-local/metadata, RFC1918 and localhost targets before a
// request is ever dispatched. The injected doer would happily answer, so a nil
// lastReq proves the guard, not the network, stopped the call.
func TestHTTPRequest_SSRFGuardRejectsPrivateTargets(t *testing.T) {
	cases := map[string]string{
		"loopback-ip":   "http://127.0.0.1/admin",
		"metadata-ip":   "http://169.254.169.254/latest/meta-data/",
		"private-10":    "http://10.0.0.5/internal",
		"private-192":   "http://192.168.1.1/",
		"localhost":     "http://localhost:8080/",
		"ipv6-loopback": "http://[::1]/",
	}
	for name, rawURL := range cases {
		t.Run(name, func(t *testing.T) {
			doer := &fakeDoer{resp: newResp(200, "should-not-run")}
			act := httpRequestAction{client: doer}
			ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

			_, err := act.Execute(ec, mustJSON(t, map[string]any{"url": rawURL}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing request")
			assert.Nil(t, doer.lastReq, "guard must block before the request is sent")
		})
	}
}

func TestHTTPRequest_AllowPrivateBypassesGuard(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	act := httpRequestAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"url":           srv.URL,
		"allow_private": true,
	}))
	require.NoError(t, err)
	assert.Equal(t, "200", res.Outputs["status"])
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits))
}

func TestHTTPRequest_HTTPTestHappyPath(t *testing.T) {
	var gotMethod, gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Test")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("done"))
	}))
	defer srv.Close()

	act := httpRequestAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"method":        "POST",
		"url":           srv.URL,
		"headers":       map[string]string{"X-Test": "v"},
		"body":          "payload",
		"allow_private": true,
	}))
	require.NoError(t, err)
	assert.Equal(t, "201", res.Outputs["status"])
	assert.Equal(t, "done", res.Outputs["body"])
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "v", gotHeader)
	assert.Equal(t, "payload", gotBody)
}

func TestHTTPRequest_HTTPTestNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	act := httpRequestAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"url": srv.URL, "allow_private": true}))
	require.NoError(t, err)
	assert.Equal(t, "503", res.Outputs["status"])
	assert.Equal(t, "false", res.Outputs["ok"])
}

func TestHTTPRequest_HTTPTestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	act := httpRequestAction{client: &http.Client{Timeout: 50 * time.Millisecond}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"url": srv.URL, "allow_private": true}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestHTTPRequest_HTTPTestCapsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("a", httpMaxResponseBytes+4096)))
	}))
	defer srv.Close()

	act := httpRequestAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"url": srv.URL, "allow_private": true}))
	require.NoError(t, err)
	assert.Len(t, res.Outputs["body"], httpMaxResponseBytes)
}

func TestHTTPRequest_ParsesJSONObjectFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ada","count":42,"ok":true}`))
	}))
	defer srv.Close()

	act := httpRequestAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"url": srv.URL, "allow_private": true}))
	require.NoError(t, err)
	assert.Equal(t, "ada", res.Outputs["json.name"])
	assert.Equal(t, "42", res.Outputs["json.count"])
	assert.Equal(t, "true", res.Outputs["json.ok"])
}
