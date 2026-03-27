package weather

import (
"encoding/json"
"fmt"
"net/http"
"net/url"
"time"
)

// AVWXClient handles calls to the AVWX API.
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

// FloatValue is a numeric field in the AVWX API (value can be null).
type FloatValue struct {
	Value *float64 `json:"value"`
	Repr  string   `json:"repr"`
}

// MetarTime represents the time field of the METAR.
type MetarTime struct {
	Repr string `json:"repr"` // e.g. "271750Z"
	Dt   string `json:"dt"`   // e.g. "2026-03-27T17:50:00Z"
}

// WxCode represents a meteorological phenomenon in the METAR (e.g. rain, fog).
type WxCode struct {
	Repr  string `json:"repr"`
	Value string `json:"value"`
}

// Cloud represents a cloud layer in the METAR.
type Cloud struct {
	Type     string   `json:"type"`     // FEW, SCT, BKN, OVC, SKC, CLR...
	Altitude *float64 `json:"altitude"` // in hundreds of feet (can be null)
	Repr     string   `json:"repr"`
}

// MetarResponse is the METAR report returned by AVWX.
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
	WindDirection FloatValue   `json:"wind_direction"` // nil if variable
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

// Runway represents an airport runway.
type Runway struct {
	Ident1   string  `json:"ident1"`
	Ident2   string  `json:"ident2"`
	Bearing1 float64 `json:"bearing1"`
	Bearing2 float64 `json:"bearing2"`
	LengthFt int     `json:"length_ft"`
}

// StationResponse represents the data of an AVWX station.
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

// StationInfo is an item from the station search results.
type StationInfo struct {
	ICAO    string `json:"icao"`
	IATA    string `json:"iata"`
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// SearchStations searches for stations by name/city. Returns at most n results.
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
