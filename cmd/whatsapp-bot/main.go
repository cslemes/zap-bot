// cmd/whatsapp-bot/main.go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cslemes/zap-bot/cmd/web"
	"github.com/cslemes/zap-bot/internal/bot"
	"github.com/joho/godotenv"
)

func main() {
	// 1. Load Configuration
	err := godotenv.Load()
	if err != nil {
		log.Println("Info: .env file not found, continuing with environment variables.")
	}
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		log.Fatal("❌ GROQ_API_KEY is not set in .env file or environment variables")
	}

	// 2. Initialize Components (Dependency Injection)
	botManager := bot.NewManager(apiKey)
	webServer := web.NewServer(botManager)

	// 3. Start the Web Server in a Goroutine
	go webServer.Start()

	// 4. Handle Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down...")
	botManager.Disconnect()
}
