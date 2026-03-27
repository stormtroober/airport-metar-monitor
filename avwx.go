package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// AVWXClient gestisce le chiamate all'API AVWX.
type AVWXClient struct {
	token      string
	httpClient *http.Client
}

func NewAVWXClient(token string) *AVWXClient {
	return &AVWXClient{
		token: token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *AVWXClient) doGet(endpoint string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, "https://avwx.rest/api"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "BEARER "+c.token)
	return c.httpClient.Do(req)
}

// FloatValue è un campo numerico dell'API AVWX (value può essere null).
type FloatValue struct {
	Value *float64 `json:"value"`
	Repr  string   `json:"repr"`
}

// MetarTime rappresenta il campo time del METAR.
type MetarTime struct {
	Repr string `json:"repr"` // es. "271750Z"
	Dt   string `json:"dt"`   // es. "2026-03-27T17:50:00Z"
}
// WxCode rappresenta un fenomeno meteorologico nel METAR (es. pioggia, nebbia).
type WxCode struct {
	Repr  string `json:"repr"`
	Value string `json:"value"`
}


// Cloud rappresenta uno strato nuvoloso nel METAR.
type Cloud struct {
	Type     string   `json:"type"`     // FEW, SCT, BKN, OVC, SKC, CLR...
	Altitude *float64 `json:"altitude"` // in centinaia di piedi (può essere null)
	Repr     string   `json:"repr"`
}

// MetarResponse è il report METAR restituito da AVWX.
type MetarResponse struct {
	Raw           string       `json:"raw"`
	Station       string       `json:"station"`
	Time          MetarTime    `json:"time"`
	Altimeter     FloatValue   `json:"altimeter"`
	Temperature   FloatValue   `json:"temperature"`
	Dewpoint      FloatValue   `json:"dewpoint"`
	Visibility    FloatValue   `json:"visibility"`
	Clouds        []Cloud      `json:"clouds"`
	WxCodes       []WxCode     `json:"wx_codes"`
	WindDirection FloatValue   `json:"wind_direction"` // nil se variabile
	WindSpeed     FloatValue   `json:"wind_speed"`
	WindGust      FloatValue   `json:"wind_gust"`
}

func (c *AVWXClient) FetchMetar(icao string) (*MetarResponse, error) {
	resp, err := c.doGet("/metar/" + icao)
	if err != nil {
		return nil, fmt.Errorf("richiesta fallita: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AVWX status %d per metar/%s", resp.StatusCode, icao)
	}
	var result MetarResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decodifica JSON: %w", err)
	}
	return &result, nil
}

// Runway rappresenta una pista di un aeroporto.
type Runway struct {
	Ident1   string  `json:"ident1"`
	Ident2   string  `json:"ident2"`
	Bearing1 float64 `json:"bearing1"`
	Bearing2 float64 `json:"bearing2"`
	LengthFt int     `json:"length_ft"`
}

// StationResponse rappresenta i dati di una stazione AVWX.
type StationResponse struct {
	ICAO      string   `json:"icao"`
	IATA      string   `json:"iata"`
	Name      string   `json:"name"`
	City      string   `json:"city"`
	Country   string   `json:"country"`
	Runways   []Runway `json:"runways"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
}

func (c *AVWXClient) FetchStation(icao string) (*StationResponse, error) {
	resp, err := c.doGet("/station/" + icao)
	if err != nil {
		return nil, fmt.Errorf("richiesta fallita: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AVWX status %d per station/%s", resp.StatusCode, icao)
	}
	var result StationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decodifica JSON: %w", err)
	}
	return &result, nil
}

// StationInfo è un elemento del risultato di ricerca stazioni.
type StationInfo struct {
	ICAO    string `json:"icao"`
	IATA    string `json:"iata"`
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// SearchStations cerca stazioni per nome/città. Restituisce al massimo n risultati.
func (c *AVWXClient) SearchStations(query string, n int) ([]StationInfo, error) {
	endpoint := fmt.Sprintf("/station/search?text=%s&n=%d", url.QueryEscape(query), n)
	resp, err := c.doGet(endpoint)
	if err != nil {
		return nil, fmt.Errorf("richiesta fallita: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AVWX status %d per search", resp.StatusCode)
	}
	var results []StationInfo
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decodifica JSON: %w", err)
	}
	return results, nil
}
