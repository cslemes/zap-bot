// internal/web/web.go
package web

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/cslemes/zap-bot/internal/bot"
)

// Server holds dependencies for the web server, like the bot manager.
type Server struct {
	botManager *bot.Manager
}

// NewServer creates a new web server with its dependencies.
func NewServer(m *bot.Manager) *Server {
	return &Server{botManager: m}
}

// Start registers handlers and starts listening.
func (s *Server) Start() {
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/status", s.handleStatus)
	http.HandleFunc("/connect", s.handleConnect)
	http.HandleFunc("/disconnect", s.handleDisconnect)

	fmt.Println("Starting web server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start web server: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Assumes index.html is in the root of the project directory
	http.ServeFile(w, r, "index.html")
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	// --- THIS IS THE RACE CONDITION FIX ---
	s.botManager.Mu.Lock()
	if s.botManager.Status == "Connected" || s.botManager.Status == "Connecting..." {
		// If already connected or connecting, just unlock and show status
		s.botManager.Mu.Unlock()
		s.handleStatus(w, r)
		return
	}
	// It's not connected, so update the status BEFORE unlocking and starting the goroutine
	s.botManager.Status = "Connecting..."
	s.botManager.Mu.Unlock()
	// ------------------------------------

	go s.botManager.ConnectClient()

	// Respond immediately with the new "Connecting..." status
	s.handleStatus(w, r)
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	s.botManager.Disconnect()
	s.handleStatus(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.botManager.Mu.Lock()

	defer s.botManager.Mu.Unlock()

	const statusTemplate = `
        <div class="status-text {{.StatusClass}}">{{.Status}}</div>
        {{if .Uptime}}
            <div>Connected for: {{.Uptime}}</div>
        {{end}}
        {{if .ShowQR}}
            <p>Scan this QR code with WhatsApp:</p>
            <img src="data:image/png;base64,{{.QrCode}}" alt="QR Code">
        {{end}}
        <div class="actions">
            {{if .ShowConnectBtn}}
                <button hx-post="/connect" hx-target="#status-box">Connect</button>
            {{end}}
            {{if .ShowDisconnectBtn}}
                <button class="disconnect-btn" hx-post="/disconnect" hx-target="#status-box">Disconnect</button>
            {{end}}
        </div>
    `
	tmpl, err := template.New("status").Parse(statusTemplate)
	if err != nil {
		http.Error(w, "Failed to parse template", http.StatusInternalServerError)
		return
	}

	uptime := ""
	if s.botManager.Status == "Connected" {
		uptime = time.Since(s.botManager.StartTime).Round(time.Second).String()
	}

	data := struct {
		Status            string
		StatusClass       string
		Uptime            string
		ShowQR            bool
		QrCode            string
		ShowConnectBtn    bool
		ShowDisconnectBtn bool
	}{
		Status:            s.botManager.Status,
		Uptime:            uptime,
		QrCode:            s.botManager.QrCode,
		ShowQR:            s.botManager.Status == "Waiting for QR Scan",
		ShowConnectBtn:    s.botManager.Status == "Disconnected" || s.botManager.Status == "Connection Failed",
		ShowDisconnectBtn: s.botManager.Status == "Connected",
	}

	switch s.botManager.Status {
	case "Connected":
		data.StatusClass = "status-connected"
	case "Disconnected", "Connection Failed":
		data.StatusClass = "status-disconnected"
	case "Waiting for QR Scan":
		data.StatusClass = "status-waiting"
	case "Connecting...":
		data.StatusClass = "status-connecting"
	}

	tmpl.Execute(w, data)
}
