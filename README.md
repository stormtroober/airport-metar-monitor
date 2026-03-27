# ✈️ Airport METAR Monitor

A Telegram bot written in Go that monitors METAR reports for your airports and alerts you automatically when new data is available. Includes crosswind and headwind analysis for each runway.

---

## Features

- 📡 Periodic METAR updates (configurable interval)
- 🛬 Headwind/crosswind analysis per runway direction
- 📊 Human-readable weather briefing (visibility, clouds, wx phenomena, temp/dewpoint spread)
- 💾 Persistent airport list per chat (JSON file)
- 🔕 Skips notifications when METAR hasn't changed

---

## Requirements

- Go 1.21+
- [AVWX API token](https://avwx.rest/) (free tier available)
- Telegram Bot token (via [@BotFather](https://t.me/BotFather))

---

## Setup

1. Clone the repo and copy the example env file:

```bash
cp .env.example .env
```

2. Fill in `.env`:

```env
AVWX_TOKEN=your_avwx_token
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
UPDATE_INTERVAL_MINUTES=60   # optional, default: 60
```

3. Run:

```bash
go run .
```

---

## Bot Commands

| Command | Description |
|---|---|
| `/start` | Initialize the bot and start periodic updates |
| `/add <ICAO>` | Add an airport (e.g. `/add EPKK`) |
| `/list` | Show registered airports |
| `/remove <ICAO>` | Remove an airport |
| `/get` | Fetch METAR for all registered airports now |
| `/get <ICAO>` | Fetch METAR for any airport (even if not registered) |
| `/metar <ICAO>` | Show raw METAR string |

---

## Project Structure

```
.
├── main.go
└── internal/
    ├── bot/        # Telegram handlers and periodic update logic
    ├── weather/    # AVWX API client and METAR formatting
    └── storage/    # JSON-based persistence
```

---

## License

MIT
