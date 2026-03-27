package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// RunwayAnalysis contiene le componenti di vento per una singola direzione di pista.
type RunwayAnalysis struct {
	Ident     string  // es. "04" o "22"
	Crosswind float64 // componente traversa in nodi (sempre ≥ 0)
	Headwind  float64 // positivo = vento di prua, negativo = vento in coda
}

// AnalyzeRunways calcola headwind/crosswind per ogni direzione (ident1 e ident2) di ogni pista.
// Restituisce nil se windDir è nil (vento variabile) o non ci sono piste.
func AnalyzeRunways(runways []Runway, windDir *float64, windSpeedKt float64) []RunwayAnalysis {
	if windDir == nil || len(runways) == 0 {
		return nil
	}
	out := make([]RunwayAnalysis, 0, len(runways)*2)
	for _, rwy := range runways {
		// Direzione 1 (es. 04)
		cross1, head1 := windComponents(*windDir, windSpeedKt, rwy.Bearing1)
		out = append(out, RunwayAnalysis{
			Ident:     rwy.Ident1,
			Crosswind: math.Abs(cross1),
			Headwind:  head1,
		})
		// Direzione 2 (es. 22, opposta)
		cross2, head2 := windComponents(*windDir, windSpeedKt, rwy.Bearing2)
		out = append(out, RunwayAnalysis{
			Ident:     rwy.Ident2,
			Crosswind: math.Abs(cross2),
			Headwind:  head2,
		})
	}
	return out
}

func windComponents(windDir, windSpeedKt, runwayHdg float64) (crosswind, headwind float64) {
	angle := (windDir - runwayHdg) * math.Pi / 180.0
	return windSpeedKt * math.Sin(angle), windSpeedKt * math.Cos(angle)
}

// FormatMetarMessage formatta il messaggio completo da inviare su Telegram (HTML).
func FormatMetarMessage(station *StationResponse, metar *MetarResponse) string {
	var sb strings.Builder

	// Intestazione
	sb.WriteString(fmt.Sprintf("✈️ <b>%s</b> — %s\n", station.ICAO, htmlEscape(station.Name)))
	if station.City != "" {
		sb.WriteString(fmt.Sprintf("📍 %s, %s\n", htmlEscape(station.City), station.Country))
	}
	sb.WriteString(fmt.Sprintf("🕐 %s\n\n", formatMetarTime(metar.Time)))

	// METAR raw
	sb.WriteString(fmt.Sprintf("<code>%s</code>\n\n", htmlEscape(metar.Raw)))

	// Vento
	sb.WriteString(fmt.Sprintf("🌬️ Vento: %s\n", formatWind(metar)))

	// Briefing meteo sintetico
	sb.WriteString(formatBriefing(metar))

	// Analisi piste
	if metar.WindDirection.Value != nil && metar.WindSpeed.Value != nil && len(station.Runways) > 0 {
		analyses := AnalyzeRunways(station.Runways, metar.WindDirection.Value, *metar.WindSpeed.Value)
		if len(analyses) > 0 {
			sb.WriteString("\n🛬 <b>Analisi piste:</b>\n")
			sb.WriteString(formatRunwayAnalyses(analyses, station.Runways))
		}
	} else if metar.WindDirection.Value == nil {
		sb.WriteString("\n⚠️ Vento variabile — analisi piste non disponibile\n")
	}

	return sb.String()
}

// formatBriefing produce un brevissimo riassunto delle condizioni meteo.
func formatBriefing(metar *MetarResponse) string {
	var parts []string

	// Visibilità
	if metar.Visibility.Value != nil {
		vis := *metar.Visibility.Value
		switch {
		case vis >= 9999:
			parts = append(parts, "visibilità ottima")
		case vis >= 5000:
			parts = append(parts, fmt.Sprintf("visibilità %.0f m", vis))
		case vis >= 1500:
			parts = append(parts, fmt.Sprintf("⚠️ visibilità ridotta %.0f m", vis))
		default:
			parts = append(parts, fmt.Sprintf("🔴 visibilità critica %.0f m", vis))
		}
	} else if metar.Visibility.Repr != "" {
		parts = append(parts, fmt.Sprintf("visibilità %s", metar.Visibility.Repr))
	}

	// Nuvole
	cloudStr := formatClouds(metar.Clouds)
	if cloudStr != "" {
		parts = append(parts, cloudStr)
	}

	// Fenomeni wx (pioggia, nebbia, etc.)
	wxStr := formatWxCodes(metar.WxCodes)
	if wxStr != "" {
		parts = append(parts, wxStr)
	}

	// Temperatura/punto di rugiada
	if metar.Temperature.Value != nil && metar.Dewpoint.Value != nil {
		spread := *metar.Temperature.Value - *metar.Dewpoint.Value
		tempStr := fmt.Sprintf("T %.0f°C / DP %.0f°C", *metar.Temperature.Value, *metar.Dewpoint.Value)
		if spread <= 2 {
			tempStr += " ⚠️ spread basso"
		}
		parts = append(parts, tempStr)
	}

	if len(parts) == 0 {
		return ""
	}
	return "📊 " + strings.Join(parts, " · ") + "\n"
}

func formatClouds(clouds []Cloud) string {
	if len(clouds) == 0 {
		return ""
	}
	// Troviamo lo strato più significativo (BKN/OVC hanno priorità)
	significant := ""
	for _, c := range clouds {
		switch c.Type {
		case "SKC", "CLR", "NSC", "NCD":
			return "cielo sereno"
		case "OVC":
			alt := ""
			if c.Altitude != nil {
				alt = fmt.Sprintf(" a %.0f ft", *c.Altitude*100)
			}
			return fmt.Sprintf("cielo coperto%s", alt)
		case "BKN":
			alt := ""
			if c.Altitude != nil {
				alt = fmt.Sprintf(" a %.0f ft", *c.Altitude*100)
			}
			significant = fmt.Sprintf("nuvolosità significativa%s", alt)
		case "SCT":
			if significant == "" {
				alt := ""
				if c.Altitude != nil {
					alt = fmt.Sprintf(" a %.0f ft", *c.Altitude*100)
				}
				significant = fmt.Sprintf("parzialmente nuvoloso%s", alt)
			}
		case "FEW":
			if significant == "" {
				significant = "poche nuvole"
			}
		}
	}
	return significant
}

func formatWxCodes(codes []WxCode) string {
	var wxParts []string
	for _, c := range codes {
		switch {
		case strings.Contains(c.Repr, "TS"):
			wxParts = append(wxParts, "⛈️ temporale")
		case strings.Contains(c.Repr, "FG"):
			wxParts = append(wxParts, "🌫️ nebbia")
		case strings.Contains(c.Repr, "RA"):
			wxParts = append(wxParts, "🌧️ pioggia")
		case strings.Contains(c.Repr, "SN"):
			wxParts = append(wxParts, "❄️ neve")
		case strings.Contains(c.Repr, "DZ"):
			wxParts = append(wxParts, "🌦️ pioggerella")
		case c.Repr != "":
			wxParts = append(wxParts, c.Repr)
		}
	}
	return strings.Join(wxParts, " ")
}

// formatRunwayAnalyses raggruppa le analisi per pista e mostra entrambe le direzioni,
// evidenziando quella preferibile per ogni pista.
func formatRunwayAnalyses(analyses []RunwayAnalysis, runways []Runway) string {
	var sb strings.Builder
	// Le analisi sono in coppia: [dir1, dir2] per ogni pista
	for i := 0; i+1 < len(analyses); i += 2 {
		a1 := analyses[i]
		a2 := analyses[i+1]

		rwyPair := a1.Ident + "/" + a2.Ident

		// La direzione preferita è quella con headwind (h > 0) e minor traverso.
		// Se entrambe hanno headwind negativo (vento in coda su entrambe), scegliamo il meno peggio.
		prefer1 := preferDirection(a1, a2)

		sb.WriteString(fmt.Sprintf("▪️ RWY <b>%s</b>\n", rwyPair))
		sb.WriteString(formatOneDirection(a1, prefer1))
		sb.WriteString(formatOneDirection(a2, !prefer1))
	}
	return sb.String()
}

// preferDirection ritorna true se a1 è preferibile ad a2.
func preferDirection(a1, a2 RunwayAnalysis) bool {
	// Headwind > 0 = vento di prua (buono). Preferiamo headwind positivo.
	a1head := a1.Headwind >= 0
	a2head := a2.Headwind >= 0
	if a1head && !a2head {
		return true
	}
	if !a1head && a2head {
		return false
	}
	// Entrambe uguali: scegliamo il minor traverso
	return a1.Crosswind <= a2.Crosswind
}

func formatOneDirection(a RunwayAnalysis, preferred bool) string {
	label := ""
	if preferred {
		label = " (preferita)"
	}

	windType := "prua"
	absHead := math.Abs(a.Headwind)
	if a.Headwind < 0 {
		windType = "coda"
	}

	return fmt.Sprintf("  RWY <b>%s</b>%s: traverso <b>%.0f kt</b> · %s %.0f kt\n",
		a.Ident, label, a.Crosswind, windType, absHead)
}

func formatWind(metar *MetarResponse) string {
	if metar.WindDirection.Value == nil {
		if metar.WindSpeed.Value != nil {
			return fmt.Sprintf("VRB @ %.0f kt", *metar.WindSpeed.Value)
		}
		return "Variabile"
	}
	s := fmt.Sprintf("%.0f° @ %.0f kt", *metar.WindDirection.Value, *metar.WindSpeed.Value)
	if metar.WindGust.Value != nil && *metar.WindGust.Value > 0 {
		s += fmt.Sprintf(" (raffiche %.0f kt)", *metar.WindGust.Value)
	}
	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// formatMetarTime converte il timestamp ISO del METAR in formato leggibile.
// Es. "2026-03-27T17:50:00Z" → "27 Mar 2026 17:50 UTC"
// Fallback al campo repr grezzo se il parsing fallisce.
func formatMetarTime(t MetarTime) string {
	if t.Dt != "" {
		parsed, err := time.Parse(time.RFC3339, t.Dt)
		if err == nil {
			return parsed.UTC().Format("02 Jan 2006 15:04 UTC")
		}
	}
	return t.Repr
}
