package bot

import (
	"context"
	"errors"
	"go.mau.fi/whatsmeow/store"
	"io"
	"net/http"
	"strings"
	"testing"
)

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshWhatsAppVersionUpdatesLoginPayload(t *testing.T) {
	originalVersion := store.GetWAVersion()
	originalPayload := store.BaseClientPayload.GetUserAgent().GetAppVersion()
	defer func() {
		store.SetWAVersion(originalVersion)
		store.BaseClientPayload.UserAgent.AppVersion = originalPayload
	}()
	client := &http.Client{Transport: versionTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "web.whatsapp.com" {
			t.Errorf("unexpected version host %q", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"client_revision":1234567890,}`)), Header: make(http.Header)}, nil
	})}
	if err := RefreshWhatsAppVersion(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if store.GetWAVersion() != (store.WAVersionContainer{2, 3000, 1234567890}) {
		t.Fatal("advertised version was not refreshed")
	}
	// This is the value serialized into registration and reconnect payloads.
	payload := store.BaseClientPayload.GetUserAgent().GetAppVersion()
	if payload.GetTertiary() != 1234567890 {
		t.Fatalf("login still advertises stale version %d", payload.GetTertiary())
	}
}
func TestRefreshWhatsAppVersionFailurePreservesVersion(t *testing.T) {
	originalVersion := store.GetWAVersion()
	for _, body := range []string{"unparseable page", `{"client_revision":0,}`} {
		client := &http.Client{Transport: versionTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		if err := RefreshWhatsAppVersion(context.Background(), client); err == nil {
			t.Errorf("accepted invalid version page %q", body)
		}
		if store.GetWAVersion() != originalVersion {
			t.Fatal("failure changed version")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: versionTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
	if err := RefreshWhatsAppVersion(ctx, client); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
