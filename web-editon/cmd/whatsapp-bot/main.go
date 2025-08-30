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

	err := godotenv.Load()
	if err != nil {
		log.Println("Info: .env file not found, continuing with environment variables.")
	}
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		log.Fatal("❌ GROQ_API_KEY is not set in .env file or environment variables")
	}

	botManager := bot.NewManager(apiKey)
	webServer := web.NewServer(botManager)

	go webServer.Start()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down...")
	botManager.Disconnect()
}
