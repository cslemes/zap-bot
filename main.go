package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/cslemes/zap-bot/internal/bot"
	"github.com/cslemes/zap-bot/internal/transcription"
	"github.com/cslemes/zap-bot/internal/web"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	terminal := flag.Bool("terminal", false, "connect immediately and print the login QR in the terminal")
	flag.Parse()
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	transcriber, err := transcription.NewClient(transcription.Config{
		Endpoint: os.Getenv("TRANSCRIPTION_ENDPOINT"), APIKey: os.Getenv("TRANSCRIPTION_API_KEY"), Model: os.Getenv("TRANSCRIPTION_MODEL"),
	})
	if err != nil {
		return err
	}
	user, password := os.Getenv("DASHBOARD_USER"), os.Getenv("DASHBOARD_PASSWORD")
	if !*terminal && (user == "" || password == "") {
		return errors.New("DASHBOARD_USER and DASHBOARD_PASSWORD are required")
	}
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "data"
	}
	transcribeGroups, err := groupTranscriptionEnabled()
	if err != nil {
		return err
	}
	versionCtx, versionCancel := context.WithTimeout(context.Background(), 15*time.Second)
	versionErr := bot.RefreshWhatsAppVersion(versionCtx, &http.Client{Timeout: 15 * time.Second})
	versionCancel()
	if versionErr != nil {
		log.Printf("Could not refresh WhatsApp Web version; using library default: %v", versionErr)
	}
	manager, err := bot.NewManager(transcriber.TranscribeAudio, dir, *terminal, transcribeGroups)
	if err != nil {
		return err
	}
	defer manager.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *terminal {
		manager.Connect()
		<-ctx.Done()
		return nil
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: web.NewHandler(manager, user, password), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	log.Printf("Dashboard listening on %s", addr)
	select {
	case err := <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
	return nil
}

func groupTranscriptionEnabled() (bool, error) {
	value := strings.TrimSpace(os.Getenv("TRANSCRIBE_GROUPS"))
	if value == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.New("TRANSCRIBE_GROUPS must be true or false")
	}
	return enabled, nil
}
