package transcription

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredEndpointAndMultipartRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/v1/audio/transcriptions" || r.Method != "POST" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing API credential")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			http.Error(w, "bad multipart", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "custom-whisper" {
			t.Errorf("wrong model: %q", r.FormValue("model"))
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Error(err)
		}
		if string(data) != "audio" || header.Filename != "audio.ogg" {
			t.Error("incorrect audio upload")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"hello world"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL + "/custom/v1/audio/transcriptions", APIKey: "test-key", Model: "custom-whisper"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.TranscribeAudio(context.Background(), []byte("audio"))
	if err != nil || text != "hello world" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}
func TestTranscriptionCancellation(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "http://127.0.0.1:1/audio/transcriptions", APIKey: "test", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.TranscribeAudio(ctx, []byte("audio"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, endpoint := range []string{"", "/relative", "ftp://host/path", "https://user:secret@host/path", "https://host/path#fragment"} {
		if _, err := NewClient(Config{Endpoint: endpoint, APIKey: "test", Model: "model"}); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	for _, config := range []Config{{Endpoint: "http://localhost/v1/audio/transcriptions", Model: "model"}, {Endpoint: "http://localhost/v1/audio/transcriptions", APIKey: "test"}} {
		if _, err := NewClient(config); err == nil {
			t.Error("accepted missing credential or model")
		}
	}
}
