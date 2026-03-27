package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	"airport-metar-monitor/internal/bot"
	"airport-metar-monitor/internal/storage"
	"airport-metar-monitor/internal/weather"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Nessun file .env trovato, uso variabili di sistema")
	}

	avwxToken := mustEnv("AVWX_TOKEN")
	telegramToken := mustEnv("TELEGRAM_BOT_TOKEN")

	intervalMinutes := 60
	if v := os.Getenv("UPDATE_INTERVAL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			intervalMinutes = n
		}
	}

	store, err := storage.NewStore("airports.json")
	if err != nil {
		log.Fatalf("Errore apertura store: %v", err)
	}

	avwx := weather.NewAVWXClient(avwxToken)
	h := bot.NewBotHandler(avwx, store, time.Duration(intervalMinutes)*time.Minute)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			if update.Message != nil {
				log.Printf("[DEFAULT] Unhandled message: %q", update.Message.Text)
			}
		}),
	}

	b, err := tgbot.New(telegramToken, opts...)
	if err != nil {
		log.Fatalf("Errore creazione bot: %v", err)
	}

	// Comandi
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/start", tgbot.MatchTypeExact, h.HandleStart)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/add", tgbot.MatchTypePrefix, h.HandleAdd)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/list", tgbot.MatchTypeExact, h.HandleList)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/remove", tgbot.MatchTypePrefix, h.HandleRemove)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/metar", tgbot.MatchTypePrefix, h.HandleMetar)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "/get", tgbot.MatchTypePrefix, h.HandleGet)

	log.Printf("✈️  Bot avviato. In attesa del comando /start dagli utenti...")
	b.Start(ctx)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Variabile d'ambiente obbligatoria mancante: %s", key)
	}
	return v
}
