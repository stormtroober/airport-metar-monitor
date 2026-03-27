package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var icaoRegexp = regexp.MustCompile(`^[A-Z]{4}$`)

// BotHandler contiene le dipendenze per i gestori Telegram.
type BotHandler struct {
	avwx     *AVWXClient
	store    *Store
	interval time.Duration
	tickers  map[int64]context.CancelFunc
	mu       sync.Mutex
}

func NewBotHandler(avwx *AVWXClient, store *Store, interval time.Duration) *BotHandler {
	return &BotHandler{
		avwx:     avwx,
		store:    store,
		interval: interval,
		tickers:  make(map[int64]context.CancelFunc),
	}
}

func commandArg(text string) string {
	// Toglie il comando (prima parola, incluso eventuale @botname) e restituisce il resto
	idx := strings.Index(text, " ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}

// HandleStart — /start
func (h *BotHandler) HandleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	
	// Avvia il ticker per l'utente
	h.startTickerForChat(ctx, b, chatID)

	// Prepara il messaggio di benvenuto e indicazione degli aeroporti in loop
	msgContext := `✈️ <b>METAR Monitor</b>

Monitoro il METAR e il vento traverso per i tuoi aeroporti, aggiornandoti automaticamente.

<b>Comandi:</b>
/add <i>ICAO</i> — Aggiungi un aeroporto
/list — Aeroporti registrati
/remove <i>ICAO</i> — Rimuovi un aeroporto
/get — Aggiorna subito tutti i tuoi aeroporti
/get <i>ICAO</i> — METAR istantaneo per qualsiasi aeroporto
/metar <i>ICAO</i> — METAR raw istantaneo`

	airports := h.store.GetAirports(chatID)
	if len(airports) > 0 {
		msgContext += "\n\n🟢 <b>Monitoraggio periodico avviato per:</b>"
		for _, a := range airports {
			msgContext += fmt.Sprintf("\n• %s — %s", a.ICAO, htmlEscape(a.Name))
		}
	} else {
		msgContext += "\n\n🟡 Nessun aeroporto registrato al momento. Usa /add per iniziare e sarai aggiornato periodicamente!"
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msgContext,
		ParseMode: models.ParseModeHTML,
	})
}

// startTickerForChat avvia un goroutine per gli aggiornamenti periodici della specifica chat.
func (h *BotHandler) startTickerForChat(ctx context.Context, b *bot.Bot, chatID int64) {
	h.mu.Lock()
	if applyCancel, ok := h.tickers[chatID]; ok {
		applyCancel()
	}
	tCtx, cancel := context.WithCancel(ctx)
	h.tickers[chatID] = cancel
	h.mu.Unlock()

	go func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-tCtx.Done():
				return
			case <-ticker.C:
				sendUpdatesForChat(tCtx, b, h.avwx, h.store, chatID)
			}
		}
	}()
}

// HandleAdd — /add <ICAO>
func (h *BotHandler) HandleAdd(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	arg := strings.TrimSpace(commandArg(update.Message.Text))
	log.Println("Add called")
	if arg == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "ℹ️ Usa: /add <ICAO>\nEs: /add EPKK",
		})
		return
	}

	upper := strings.ToUpper(arg)
	if icaoRegexp.MatchString(upper) {
		h.addByICAO(ctx, b, chatID, upper)
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "❌ Formato ICAO non valido. Usa un codice di 4 lettere (es. LIRF).",
		})
	}
}

// addByICAO recupera la stazione e la aggiunge allo store.
func (h *BotHandler) addByICAO(ctx context.Context, b *bot.Bot, chatID int64, icao string) {
	station, err := h.avwx.FetchStation(icao)
	if err != nil {
		log.Printf("[add] FetchStation(%s): %v", icao, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("❌ Stazione <b>%s</b> non trovata. Controlla il codice ICAO.", icao),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	added, err := h.store.AddAirport(chatID, Airport{ICAO: station.ICAO, Name: station.Name, City: station.City})
	if err != nil {
		log.Printf("[add] store error: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ Errore nel salvataggio."})
		return
	}
	if !added {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("ℹ️ <b>%s</b> è già nella tua lista.", station.ICAO),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("✅ <b>%s</b> — %s aggiunto! Ecco il METAR attuale:", station.ICAO, htmlEscape(station.Name)),
		ParseMode: models.ParseModeHTML,
	})

	// Invia il METAR immediato
	msg, err := buildMetarMessage(h.avwx, station.ICAO)
	if err != nil {
		log.Printf("[add] buildMetarMessage(%s): %v", station.ICAO, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "⚠️ Aggiunto, ma impossibile recuperare il METAR al momento.",
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msg,
		ParseMode: models.ParseModeHTML,
	})
}


// HandleList — /list
func (h *BotHandler) HandleList(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	airports := h.store.GetAirports(chatID)
	if len(airports) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "📋 Nessun aeroporto registrato. Usa /add per aggiungerne uno.",
		})
		return
	}
	var sb strings.Builder
	sb.WriteString("📋 <b>Aeroporti registrati:</b>\n\n")
	for _, a := range airports {
		sb.WriteString(fmt.Sprintf("• <b>%s</b> — %s\n", a.ICAO, htmlEscape(a.Name)))
	}
	sb.WriteString("\nUsa /remove <i>ICAO</i> per rimuoverne uno.")
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      sb.String(),
		ParseMode: models.ParseModeHTML,
	})
}

// HandleRemove — /remove <ICAO>
func (h *BotHandler) HandleRemove(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	icao := strings.ToUpper(strings.TrimSpace(commandArg(update.Message.Text)))
	if icao == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "ℹ️ Usa: /remove <ICAO>"})
		return
	}
	removed, err := h.store.RemoveAirport(chatID, icao)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ Errore durante la rimozione."})
		return
	}
	if !removed {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("ℹ️ <b>%s</b> non era nella tua lista.", icao),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("🗑️ <b>%s</b> rimosso.", icao),
		ParseMode: models.ParseModeHTML,
	})
}

// HandleMetar — /metar <ICAO>
func (h *BotHandler) HandleMetar(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	icao := strings.ToUpper(strings.TrimSpace(commandArg(update.Message.Text)))
	if !icaoRegexp.MatchString(icao) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "ℹ️ Usa: /metar <ICAO> (es. /metar EPKK)",
		})
		return
	}
	msg, err := buildMetarMessage(h.avwx, icao)
	if err != nil {
		log.Printf("[metar] %s: %v", icao, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("❌ Errore METAR per <b>%s</b>.", htmlEscape(icao)),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msg,
		ParseMode: models.ParseModeHTML,
	})
}

// buildMetarMessage recupera METAR e stazione e costruisce il messaggio.
func buildMetarMessage(avwx *AVWXClient, icao string) (string, error) {
	metar, err := avwx.FetchMetar(icao)
	if err != nil {
		return "", fmt.Errorf("METAR: %w", err)
	}
	station, err := avwx.FetchStation(icao)
	if err != nil {
		return "", fmt.Errorf("station: %w", err)
	}
	return FormatMetarMessage(station, metar), nil
}

// HandleGet — /get [ICAO]
// Senza argomento: aggiorna tutti gli aeroporti registrati per questa chat.
// Con ICAO: recupera il METAR di quell'aeroporto specifico (anche non in lista).
func (h *BotHandler) HandleGet(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	arg := strings.ToUpper(strings.TrimSpace(commandArg(update.Message.Text)))

	if icaoRegexp.MatchString(arg) {
		// ICAO specifico
		msg, err := buildMetarMessage(h.avwx, arg)
		if err != nil {
			log.Printf("[get] %s: %v", arg, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf("❌ Impossibile recuperare METAR per <b>%s</b>.", htmlEscape(arg)),
				ParseMode: models.ParseModeHTML,
			})
			return
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      msg,
			ParseMode: models.ParseModeHTML,
		})
		return
	}

	// Nessun argomento (o argomento non valido): aggiorna tutti gli aeroporti della chat
	airports := h.store.GetAirports(chatID)
	if len(airports) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "📋 Nessun aeroporto registrato. Usa /add <ICAO> per aggiungerne uno.",
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("🔄 Aggiornamento in corso per %d aeroporti...", len(airports)),
	})
	for _, airport := range airports {
		msg, err := buildMetarMessage(h.avwx, airport.ICAO)
		if err != nil {
			log.Printf("[get] %s: %v", airport.ICAO, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf("❌ Errore METAR per <b>%s</b>.", htmlEscape(airport.ICAO)),
				ParseMode: models.ParseModeHTML,
			})
			continue
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      msg,
			ParseMode: models.ParseModeHTML,
		})
	}
}
