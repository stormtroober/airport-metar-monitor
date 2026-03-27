package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
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

	store, err := NewStore("airports.json")
	if err != nil {
		log.Fatalf("Errore apertura store: %v", err)
	}

	avwx := NewAVWXClient(avwxToken)
	h := NewBotHandler(avwx, store, time.Duration(intervalMinutes)*time.Minute)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			if update.Message != nil {
				log.Printf("[DEFAULT] Unhandled message: %q", update.Message.Text)
			}
		}),
	}

	b, err := bot.New(telegramToken, opts...)
	if err != nil {
		log.Fatalf("Errore creazione bot: %v", err)
	}

	// Comandi
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, h.HandleStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/add", bot.MatchTypePrefix, h.HandleAdd)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/list", bot.MatchTypeExact, h.HandleList)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/remove", bot.MatchTypePrefix, h.HandleRemove)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/metar", bot.MatchTypePrefix, h.HandleMetar)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/get", bot.MatchTypePrefix, h.HandleGet)

	log.Printf("✈️  Bot avviato. In attesa del comando /start dagli utenti...")
	b.Start(ctx)
}

// sendUpdatesForChat invia il METAR per ogni aeroporto di una specifica chat,
// saltando gli aeroporti il cui METAR non è cambiato dall'ultimo invio.
func sendUpdatesForChat(ctx context.Context, b *bot.Bot, avwx *AVWXClient, store *Store, chatID int64) {
	airports := store.GetAirports(chatID)
	for _, airport := range airports {
		metar, station, err := fetchMetarData(avwx, airport.ICAO)
		if err != nil {
			log.Printf("[ticker] %s per chat %d: %v", airport.ICAO, chatID, err)
			continue
		}
		if !store.IsNewMetar(chatID, airport.ICAO, metar.Time.Repr) {
			log.Printf("[ticker] %s per chat %d: nessun aggiornamento (stesso timestamp)", airport.ICAO, chatID)
			
			// Invia il messaggio di avviso
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf("ℹ️ Nessun nuovo METAR per <b>%s</b>. L'ultimo bollettino risale alle %s.", airport.ICAO, formatMetarTime(metar.Time)),
				ParseMode: "HTML",
			})
			continue
		}
		msg := FormatMetarMessage(station, metar)
		_, err = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      msg,
			ParseMode: "HTML",
		})
		if err != nil {
			log.Printf("[ticker] SendMessage a %d: %v", chatID, err)
		}
	}
}

// fetchMetarData recupera METAR e stazione in parallelo.
func fetchMetarData(avwx *AVWXClient, icao string) (*MetarResponse, *StationResponse, error) {
	metar, err := avwx.FetchMetar(icao)
	if err != nil {
		return nil, nil, fmt.Errorf("METAR: %w", err)
	}
	station, err := avwx.FetchStation(icao)
	if err != nil {
		return nil, nil, fmt.Errorf("station: %w", err)
	}
	return metar, station, nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Variabile d'ambiente obbligatoria mancante: %s", key)
	}
	return v
}
