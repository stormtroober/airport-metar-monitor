package bot

import (
"context"
"fmt"
"log"
"regexp"
"strings"
"sync"
"time"

"airport-metar-monitor/internal/storage"
"airport-metar-monitor/internal/weather"

"github.com/go-telegram/bot"
"github.com/go-telegram/bot/models"
)

var icaoRegexp = regexp.MustCompile(`^[A-Z]{4}$`)

// BotHandler contains dependencies for Telegram handlers.
type BotHandler struct {
	avwx     *weather.AVWXClient
	store    *storage.Store
	interval time.Duration
	tickers  map[int64]context.CancelFunc
	mu       sync.Mutex
}

func NewBotHandler(avwx *weather.AVWXClient, store *storage.Store, interval time.Duration) *BotHandler {
	return &BotHandler{
		avwx:     avwx,
		store:    store,
		interval: interval,
		tickers:  make(map[int64]context.CancelFunc),
	}
}

func commandArg(text string) string {
	// Removes the command (first word, including optional @botname) and returns the rest
	idx := strings.Index(text, " ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}

// HandleStart — /start
func (h *BotHandler) HandleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID

	// Start the ticker for the user
	h.startTickerForChat(ctx, b, chatID)

	// Prepares the welcome message and lists monitored airports
	msgContext := `✈️ <b>METAR Monitor</b>

I monitor METAR and crosswind for your airports, updating you automatically.

<b>Commands:</b>
/add <i>ICAO</i> — Add an airport
/list — Registered airports
/remove <i>ICAO</i> — Remove an airport
/get — Update all your airports now
/get <i>ICAO</i> — Instant METAR for any airport
/metar <i>ICAO</i> — Instant raw METAR`

	airports := h.store.GetAirports(chatID)
	if len(airports) > 0 {
msgContext += "\n\n✅ <b>Periodic monitoring started for:</b>"
		for _, a := range airports {
			msgContext += fmt.Sprintf("\n• %s — %s", a.ICAO, weather.HTMLEscape(a.Name))
		}
	} else {
msgContext += "\n\n🟡 No airports registered at the moment. Use /add to start and you will be updated periodically!"
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msgContext,
		ParseMode: models.ParseModeHTML,
	})
}

// startTickerForChat starts a goroutine for periodic updates for a specific chat.
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
				h.sendUpdatesForChat(tCtx, b, chatID)
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
			Text:   "ℹ️ Usage: /add <ICAO>\nExample: /add EPKK",
		})
		return
	}

	upper := strings.ToUpper(arg)
	if icaoRegexp.MatchString(upper) {
		h.addByICAO(ctx, b, chatID, upper)
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "❌ Invalid ICAO format. Use a 4-letter code (e.g. LIRF).",
		})
	}
}

// addByICAO fetches the station and adds it to the store.
func (h *BotHandler) addByICAO(ctx context.Context, b *bot.Bot, chatID int64, icao string) {
	station, err := h.avwx.FetchStation(icao)
	if err != nil {
		log.Printf("[add] FetchStation(%s): %v", icao, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("❌ Station <b>%s</b> not found. Check the ICAO code.", icao),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	added, err := h.store.AddAirport(chatID, storage.Airport{ICAO: station.ICAO, Name: station.Name, City: station.City})
	if err != nil {
		log.Printf("[add] store error: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ Error while saving."})
		return
	}
	if !added {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("ℹ️ <b>%s</b> is already in your list.", station.ICAO),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("✅ <b>%s</b> — %s added! Here is the current METAR:", station.ICAO, weather.HTMLEscape(station.Name)),
		ParseMode: models.ParseModeHTML,
	})

	// Send immediate METAR
	msg, err := h.buildMetarMessage(station.ICAO)
	if err != nil {
		log.Printf("[add] buildMetarMessage(%s): %v", station.ICAO, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "⚠️ Added, but unable to retrieve METAR at the moment.",
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
			Text:   "📋 No airports registered. Use /add to add one.",
		})
		return
	}
	var sb strings.Builder
	sb.WriteString("📋 <b>Registered airports:</b>\n\n")
	for _, a := range airports {
		sb.WriteString(fmt.Sprintf("• <b>%s</b> — %s\n", a.ICAO, weather.HTMLEscape(a.Name)))
	}
	sb.WriteString("\nUse /remove <i>ICAO</i> to remove one.")
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
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "ℹ️ Usage: /remove <ICAO>"})
		return
	}
	removed, err := h.store.RemoveAirport(chatID, icao)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "❌ Error during removal."})
		return
	}
	if !removed {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("ℹ️ <b>%s</b> was not in your list.", icao),
			ParseMode: models.ParseModeHTML,
		})
		return
	}
		b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("🗑️ <b>%s</b> removed.", icao),
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
			Text:   "ℹ️ Usage: /metar <ICAO> (e.g. /metar EPKK)",
		})
		return
	}
	msg, err := h.buildMetarMessage(icao)
	if err != nil {
		log.Printf("[metar] %s: %v", icao, err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("❌ METAR error for <b>%s</b>.", weather.HTMLEscape(icao)),
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

// buildMetarMessage fetches METAR and station and builds the message.
func (h *BotHandler) buildMetarMessage(icao string) (string, error) {
	metar, err := h.avwx.FetchMetar(icao)
	if err != nil {
		return "", fmt.Errorf("METAR: %w", err)
	}
	station, err := h.avwx.FetchStation(icao)
	if err != nil {
		return "", fmt.Errorf("station: %w", err)
	}
	return weather.FormatMetarMessage(station, metar), nil
}

// HandleGet — /get [ICAO]
// Without argument: updates all registered airports for this chat.
// With ICAO: fetches METAR for that specific airport (even if not in list).
func (h *BotHandler) HandleGet(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	arg := strings.ToUpper(strings.TrimSpace(commandArg(update.Message.Text)))

	if icaoRegexp.MatchString(arg) {
		// Specific ICAO
		msg, err := h.buildMetarMessage(arg)
		if err != nil {
			log.Printf("[get] %s: %v", arg, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf("❌ Unable to retrieve METAR for <b>%s</b>.", weather.HTMLEscape(arg)),
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

	// No argument (or invalid argument): updates all airports for the chat
	airports := h.store.GetAirports(chatID)
	if len(airports) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "📋 No airports registered. Use /add <ICAO> to add one.",
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("🔄 Updating %d airports...", len(airports)),
	})
	for _, airport := range airports {
		msg, err := h.buildMetarMessage(airport.ICAO)
		if err != nil {
			log.Printf("[get] %s: %v", airport.ICAO, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf("❌ METAR error for <b>%s</b>.", weather.HTMLEscape(airport.ICAO)),
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

// sendUpdatesForChat sends the METAR for each airport in a specific chat,
// skipping airports whose METAR hasn't changed since the last sending.
func (h *BotHandler) sendUpdatesForChat(ctx context.Context, b *bot.Bot, chatID int64) {
airports := h.store.GetAirports(chatID)
for _, airport := range airports {
metar, station, err := h.fetchMetarData(airport.ICAO)
if err != nil {
log.Printf("[ticker] %s for chat %d: %v", airport.ICAO, chatID, err)
continue
}
if !h.store.IsNewMetar(chatID, airport.ICAO, metar.Time.Repr) {
log.Printf("[ticker] %s for chat %d: no update (same timestamp)", airport.ICAO, chatID)

// Send notice message
b.SendMessage(ctx, &bot.SendMessageParams{
ChatID:    chatID,
Text:      fmt.Sprintf("ℹ️ No new METAR for <b>%s</b>. Latest report is from %s.", airport.ICAO, weather.FormatMetarTime(metar.Time)),
ParseMode: "HTML",
})
continue
}
msg := weather.FormatMetarMessage(station, metar)
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

// fetchMetarData fetches METAR and station data.
func (h *BotHandler) fetchMetarData(icao string) (*weather.MetarResponse, *weather.StationResponse, error) {
metar, err := h.avwx.FetchMetar(icao)
if err != nil {
return nil, nil, fmt.Errorf("METAR: %w", err)
}
station, err := h.avwx.FetchStation(icao)
if err != nil {
return nil, nil, fmt.Errorf("station: %w", err)
}
return metar, station, nil
}
