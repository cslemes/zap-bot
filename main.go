package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log"

	_ "github.com/mattn/go-sqlite3"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"

	"github.com/joho/godotenv"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Struct for Groq API response
type GroqTranscriptionResponse struct {
	Text string `json:"text"`
}

func transcribeAudio(audioData []byte, apiKey string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the audio file directly from memory (no temp file needed)
	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err = io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("failed to copy audio content: %w", err)
	}

	// Model field
	if err := writer.WriteField("model", "whisper-large-v3"); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}
	writer.Close()

	// Build HTTP request
	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/audio/transcriptions", body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, errMsg)
	}

	var groqResp GroqTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&groqResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	return groqResp.Text, nil
}

func handleMessages(ctx context.Context, client *whatsmeow.Client, evt interface{}, apiKey string) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Message.GetAudioMessage() != nil {
			fmt.Println("🎙️ Received voice note from", v.Info.Sender.String())

			// Download audio
			audioData, err := client.Download(ctx, v.Message.GetAudioMessage())
			if err != nil {
				fmt.Println("Error downloading audio:", err)
				return
			}

			// Call Groq transcription
			transcript, err := transcribeAudio(audioData, apiKey)
			if err != nil {
				fmt.Println("❌ Transcription error:", err)
				return
			}

			// Build reply
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
				fmt.Println("Error sending message:", err)
			}
		}
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ Please set GROQ_API_KEY environment variable")
		return
	}

	dbLog := waLog.Stdout("Database", "DEBUG", true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", "file:login-store.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Register event handler
	client.AddEventHandler(func(evt interface{}) {
		// Context with timeout per event
		evtCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		handleMessages(evtCtx, client, evt, apiKey)
	})

	// Login & QR flow
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		if err := client.Connect(); err != nil {
			panic(err)
		}
		defer client.Disconnect()

		for evt := range qrChan {
			if evt.Event == "code" {
				qrCode, err := qrcode.New(evt.Code, qrcode.Medium)
				if err != nil {
					fmt.Println("Error generating QR code:", err)
					continue
				}
				fmt.Println("Scan this QR code with WhatsApp app:")
				fmt.Println(qrCode.ToString(true))
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		if err := client.Connect(); err != nil {
			panic(err)
		}
		defer client.Disconnect()
	}

	// Wait for Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
