// internal/bot/bot.go
package bot

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	qrcode "github.com/skip2/go-qrcode"
	// Import your new internal package
	"github.com/cslemes/zap-bot/internal/groq"
)

// Manager holds the client and state, safe for concurrent access.
// It and its fields are exported to be accessible by the web and main packages.
type Manager struct {
	Client    *whatsmeow.Client
	Mu        sync.Mutex
	Status    string
	QrCode    string
	APIKey    string
	StartTime time.Time
}

// NewManager is a constructor for the Manager, an idiomatic Go practice.
func NewManager(apiKey string) *Manager {
	return &Manager{
		Status: "Disconnected",
		APIKey: apiKey,
	}
}

// ConnectClient initializes and connects the WhatsApp client.
func (m *Manager) ConnectClient() {
	m.updateStatus("Connecting...", "")

	ctx := context.Background()

	dbLog := waLog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New(ctx, "sqlite3", "file:login-store.db?_foreign_keys=on", dbLog)
	if err != nil {
		log.Printf("Error creating SQL container: %v", err)
		m.updateStatus("Connection Failed", "")
		return
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Printf("Error getting first device: %v", err)
		m.updateStatus("Connection Failed", "")
		return
	}

	// --- THIS IS THE FIX ---
	// If no device is found, create a new one
	if deviceStore == nil {
		deviceStore = container.NewDevice()
	}
	// -----------------------

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	m.Client = client
	m.Client.AddEventHandler(m.eventHandler)

	// The rest of the function remains the same
	if m.Client.Store.ID == nil {
		qrChan, _ := m.Client.GetQRChannel(context.Background())
		err = m.Client.Connect()
		if err != nil {
			log.Printf("Error connecting: %v", err)
			m.updateStatus("Connection Failed", "")
			return
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				png, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if err == nil {
					qrBase64 := base64.StdEncoding.EncodeToString(png)
					m.updateStatus("Waiting for QR Scan", qrBase64)
				}
			} else if evt.Event == "success" {
				m.StartTime = time.Now()
				m.updateStatus("Connected", "")
			} else {
				log.Printf("Login event: %s", evt.Event)
			}
		}
	} else {
		err = m.Client.Connect()
		if err != nil {
			log.Printf("Error connecting: %v", err)
			m.updateStatus("Connection Failed", "")
			return
		}
		m.StartTime = time.Now()
		m.updateStatus("Connected", "")
	}
}

// Disconnect the client.
func (m *Manager) Disconnect() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if m.Client != nil && (m.Client.IsConnected()) {
		m.Client.Disconnect()
		m.Status = "Disconnected"
		m.QrCode = ""
	}
}

func (m *Manager) updateStatus(status, qrCode string) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Status = status
	m.QrCode = qrCode
}

func (m *Manager) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Disconnected:
		m.updateStatus("Disconnected", "")
	case *events.Message:
		evtCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// This now calls the local handleMessages function
		handleMessages(evtCtx, m.Client, v, m.APIKey)
	}
}

// handleMessages is now an un-exported function within the bot package.
func handleMessages(ctx context.Context, client *whatsmeow.Client, v *events.Message, apiKey string) {
	if v.Message.GetAudioMessage() == nil {
		return
	}
	log.Println("🎙️ Received voice note from", v.Info.Sender.String())

	audioData, err := client.Download(ctx, v.Message.GetAudioMessage())
	if err != nil {
		log.Println("Error downloading audio:", err)
		return
	}

	// It now calls the exported function from your groq package
	transcript, err := groq.TranscribeAudio(audioData, apiKey)
	if err != nil {
		log.Println("❌ Transcription error:", err)
		return
	}

	messageText := fmt.Sprintf("🎙️ *Transcrição do áudio:*\n\n\"%s\"\n\n_Powered by Cris AI 🤖_", transcript)

	quotedInfo := &waProto.ContextInfo{
		QuotedMessage: v.Message,
		Participant:   proto.String(v.Info.Sender.String()),
		StanzaID:      proto.String(v.Info.ID),
	}

	_, err = client.SendMessage(ctx, v.Info.Sender, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text:        proto.String(messageText),
			ContextInfo: quotedInfo,
		},
	})
	if err != nil {
		log.Println("Error sending message:", err)
	}
}
