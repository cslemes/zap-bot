package web

import (
	"github.com/cslemes/zap-bot/internal/bot"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeManager struct{ connects, disconnects int }

func (m *fakeManager) Snapshot() bot.State { return bot.State{Status: "Disconnected"} }
func (m *fakeManager) Connect()            { m.connects++ }
func (m *fakeManager) Disconnect()         { m.disconnects++ }
func TestDashboardAccessAndControls(t *testing.T) {
	m := &fakeManager{}
	handler := NewHandler(m, "admin", "password")
	cases := []struct {
		method, path string
		auth, header bool
		want         int
	}{
		{"GET", "/", false, false, 401}, {"GET", "/status", false, false, 401},
		{"GET", "/", true, false, 200}, {"GET", "/status", true, false, 200},
		{"GET", "/connect", true, false, 405}, {"POST", "/connect", true, false, 403},
		{"POST", "/connect", true, true, 200}, {"POST", "/disconnect", true, true, 200},
	}
	for _, c := range cases {
		t.Run(c.method+c.path+http.StatusText(c.want), func(t *testing.T) {
			r := httptest.NewRequest(c.method, c.path, nil)
			if c.auth {
				r.SetBasicAuth("admin", "password")
			}
			if c.header {
				r.Header.Set("X-Zap-Bot", "1")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("want %d got %d: %s", c.want, w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive response can be cached")
			}
		})
	}
	if m.connects != 1 || m.disconnects != 1 {
		t.Fatal("unauthorized control request reached manager")
	}
}
