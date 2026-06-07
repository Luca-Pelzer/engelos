package elevenlabs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	base := []Option{WithBaseURL(srv.URL), WithAPIKey("test-key")}
	return New(append(base, opts...)...)
}

func TestSynthesizeReturnsAudioBytes(t *testing.T) {
	var gotPath, gotKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte{0xff, 0xfb, 0x10, 0x00})
	}))
	defer srv.Close()

	c := testClient(t, srv)
	audio, err := c.Synthesize(context.Background(), "voice123", "hello world")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(audio) != 4 {
		t.Fatalf("expected 4 audio bytes, got %d", len(audio))
	}
	if gotPath != "/v1/text-to-speech/voice123" {
		t.Fatalf("bad path: %s", gotPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("bad key header: %s", gotKey)
	}
	if !strings.Contains(gotBody, `"model_id":"eleven_flash_v2_5"`) {
		t.Fatalf("body missing model: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"text":"hello world"`) {
		t.Fatalf("body missing text: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"similarity_boost":0.75`) {
		t.Fatalf("body missing voice settings: %s", gotBody)
	}
}

func TestSynthesizeEmptyTextNoRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	c := testClient(t, srv)
	audio, err := c.Synthesize(context.Background(), "v", "   ")
	if err != nil || audio != nil {
		t.Fatalf("expected (nil,nil), got (%v,%v)", audio, err)
	}
	if called {
		t.Fatal("server should not be called for empty text")
	}
}

func TestSynthesizeMissingKey(t *testing.T) {
	c := New(WithBaseURL("http://unused"))
	_, err := c.Synthesize(context.Background(), "v", "hi")
	if !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("got %v want ErrAPIKeyRequired", err)
	}
}

func TestSynthesizeMissingVoice(t *testing.T) {
	c := New(WithBaseURL("http://unused"), WithAPIKey("k"))
	_, err := c.Synthesize(context.Background(), "  ", "hi")
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("got %v want ErrAPI", err)
	}
}

func TestSynthesizeUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	_, err := c.Synthesize(context.Background(), "v", "hi")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v want ErrUnauthorized", err)
	}
}

func TestSynthesizeQuotaExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	_, err := c.Synthesize(context.Background(), "v", "hi")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("got %v want ErrQuotaExceeded", err)
	}
}

func TestSynthesizeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"detail":"boom"}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	_, err := c.Synthesize(context.Background(), "v", "hi")
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("got %v want ErrAPI", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should carry detail: %v", err)
	}
}

func TestVoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/voices" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"voices":[{"voice_id":"v1","name":"George","category":"premade"}]}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	voices, err := c.Voices(context.Background())
	if err != nil {
		t.Fatalf("Voices: %v", err)
	}
	if len(voices) != 1 || voices[0].ID != "v1" || voices[0].Name != "George" {
		t.Fatalf("bad voices: %+v", voices)
	}
}

func TestVerifyKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"subscription":{"tier":"creator","character_count":100,"character_limit":1000}}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	sub, err := c.VerifyKey(context.Background())
	if err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
	if sub.Tier != "creator" || sub.CharacterCount != 100 || sub.CharacterLimit != 1000 {
		t.Fatalf("bad subscription: %+v", sub)
	}
}

func TestVoicesMissingKey(t *testing.T) {
	c := New()
	_, err := c.Voices(context.Background())
	if !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("got %v want ErrAPIKeyRequired", err)
	}
}

func TestCreateVoiceClone(t *testing.T) {
	var gotPath, gotKey, gotName string
	var gotFilenames []string
	var gotFileData []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotName = r.FormValue("name")
		for _, fhs := range r.MultipartForm.File["files"] {
			gotFilenames = append(gotFilenames, fhs.Filename)
			f, _ := fhs.Open()
			b, _ := io.ReadAll(f)
			_ = f.Close()
			gotFileData = append(gotFileData, string(b))
		}
		_, _ = io.WriteString(w, `{"voice_id":"newvoice123","requires_verification":false}`)
	}))
	defer srv.Close()

	c := testClient(t, srv)
	got, err := c.CreateVoiceClone(context.Background(), "My Clone", []VoiceSample{
		{Filename: "a.mp3", Data: []byte("audio-a")},
		{Filename: "b.mp3", Data: []byte("audio-b")},
	})
	if err != nil {
		t.Fatalf("CreateVoiceClone: %v", err)
	}
	if got.VoiceID != "newvoice123" || got.RequiresVerification {
		t.Fatalf("bad result: %+v", got)
	}
	if gotPath != "/v1/voices/add" {
		t.Fatalf("bad path: %s", gotPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("bad key header: %s", gotKey)
	}
	if gotName != "My Clone" {
		t.Fatalf("bad name: %s", gotName)
	}
	if len(gotFilenames) != 2 || gotFilenames[0] != "a.mp3" || gotFilenames[1] != "b.mp3" {
		t.Fatalf("bad filenames: %v", gotFilenames)
	}
	if len(gotFileData) != 2 || gotFileData[0] != "audio-a" || gotFileData[1] != "audio-b" {
		t.Fatalf("bad file data: %v", gotFileData)
	}
}

func TestCreateVoiceCloneSkipsEmptySamples(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		count = len(r.MultipartForm.File["files"])
		_, _ = io.WriteString(w, `{"voice_id":"v","requires_verification":true}`)
	}))
	defer srv.Close()

	c := testClient(t, srv)
	got, err := c.CreateVoiceClone(context.Background(), "n", []VoiceSample{
		{Filename: "empty.mp3", Data: nil},
		{Filename: "real.mp3", Data: []byte("x")},
	})
	if err != nil {
		t.Fatalf("CreateVoiceClone: %v", err)
	}
	if !got.RequiresVerification {
		t.Fatalf("expected requires_verification true")
	}
	if count != 1 {
		t.Fatalf("expected 1 uploaded file, got %d", count)
	}
}

func TestCreateVoiceCloneValidation(t *testing.T) {
	c := New(WithBaseURL("http://unused"), WithAPIKey("k"))
	if _, err := c.CreateVoiceClone(context.Background(), "  ", []VoiceSample{{Data: []byte("x")}}); !errors.Is(err, ErrAPI) {
		t.Fatalf("empty name: got %v want ErrAPI", err)
	}
	if _, err := c.CreateVoiceClone(context.Background(), "n", nil); !errors.Is(err, ErrAPI) {
		t.Fatalf("no samples: got %v want ErrAPI", err)
	}
	if _, err := c.CreateVoiceClone(context.Background(), "n", []VoiceSample{{Data: nil}}); !errors.Is(err, ErrAPI) {
		t.Fatalf("all-empty samples: got %v want ErrAPI", err)
	}
	nokey := New(WithBaseURL("http://unused"))
	if _, err := nokey.CreateVoiceClone(context.Background(), "n", []VoiceSample{{Data: []byte("x")}}); !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("missing key: got %v want ErrAPIKeyRequired", err)
	}
}

func TestCreateVoiceCloneNoVoiceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"requires_verification":false}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	if _, err := c.CreateVoiceClone(context.Background(), "n", []VoiceSample{{Data: []byte("x")}}); !errors.Is(err, ErrAPI) {
		t.Fatalf("got %v want ErrAPI", err)
	}
}

func TestDeleteVoice(t *testing.T) {
	var gotPath, gotMethod, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotKey = r.Header.Get("xi-api-key")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	if err := c.DeleteVoice(context.Background(), "voice123"); err != nil {
		t.Fatalf("DeleteVoice: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("bad method: %s", gotMethod)
	}
	if gotPath != "/v1/voices/voice123" {
		t.Fatalf("bad path: %s", gotPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("bad key header: %s", gotKey)
	}
}

func TestDeleteVoiceValidation(t *testing.T) {
	c := New(WithBaseURL("http://unused"), WithAPIKey("k"))
	if err := c.DeleteVoice(context.Background(), "  "); !errors.Is(err, ErrAPI) {
		t.Fatalf("empty id: got %v want ErrAPI", err)
	}
	nokey := New(WithBaseURL("http://unused"))
	if err := nokey.DeleteVoice(context.Background(), "v"); !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("missing key: got %v want ErrAPIKeyRequired", err)
	}
}

func TestDeleteVoiceServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"detail":"nope"}`)
	}))
	defer srv.Close()
	c := testClient(t, srv)
	if err := c.DeleteVoice(context.Background(), "v"); !errors.Is(err, ErrAPI) {
		t.Fatalf("got %v want ErrAPI", err)
	}
}
