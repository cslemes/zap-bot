package web

import (
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"github.com/cslemes/zap-bot/internal/bot"
	"net/http"
	"time"
)

//go:embed index.html
var index []byte

type manager interface {
	Snapshot() bot.State
	Connect()
	Disconnect()
}

func NewHandler(m manager, user, password string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	status := func(w http.ResponseWriter, r *http.Request) {
		state := m.Snapshot()
		uptime := ""
		if !state.Started.IsZero() {
			uptime = time.Since(state.Started).Round(time.Second).String()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct{ Status, QR, Uptime string }{state.Status, state.QR, uptime})
	}
	mux.HandleFunc("GET /status", status)
	mux.HandleFunc("POST /connect", func(w http.ResponseWriter, r *http.Request) { m.Connect(); status(w, r) })
	mux.HandleFunc("POST /disconnect", func(w http.ResponseWriter, r *http.Request) { m.Disconnect(); status(w, r) })
	expectedUser, expectedPassword := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		u, p, ok := r.BasicAuth()
		uh, ph := sha256.Sum256([]byte(u)), sha256.Sum256([]byte(p))
		if user == "" || password == "" || !ok || subtle.ConstantTimeCompare(uh[:], expectedUser[:])&subtle.ConstantTimeCompare(ph[:], expectedPassword[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Zap Bot", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-Zap-Bot") != "1" {
			http.Error(w, "Missing dashboard request header", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
