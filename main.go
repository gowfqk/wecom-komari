package main

import (
	"log"
	"net/http"
	"os"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "[wecom-komari] ", log.LstdFlags|log.Lshortfile)
}

func main() {
	logger.Println("Starting server on :8080")
	http.HandleFunc("/wecomchan", recoverMiddleware(wecomChanHandler))
	http.HandleFunc("/mail", recoverMiddleware(mailHandler))
	http.HandleFunc("/callback", recoverMiddleware(wecomCallbackHandler))

	// Telegram push (always available, uses sendkey auth)
	http.HandleFunc("/telegram/push", recoverMiddleware(telegramPushHandler))
	logger.Println("Registered: /telegram/push")

	// Telegram webhook (needs bot token)
	if TelegramBotToken != "" {
		http.HandleFunc("/telegram/webhook", recoverMiddleware(telegramWebhookHandler))
		logger.Println("Registered: /telegram/webhook")
		if err := setTelegramBotCommands(); err != nil {
			logger.Printf("Set bot commands failed: %v", err)
		}
	}

	// Webhook endpoint
	http.HandleFunc("/webhook", recoverMiddleware(webhookHandler))
	logger.Println("Registered: /webhook")

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ready")) })
	srv := &http.Server{Addr: ":8080", ReadTimeout: 15e9, WriteTimeout: 15e9, IdleTimeout: 60e9}
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatal(err)
	}
}
