package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Airport rappresenta un aeroporto registrato da un utente.
type Airport struct {
	ICAO string `json:"icao"`
	Name string `json:"name"`
	City string `json:"city"`
}

type storeData struct {
	Airports map[int64][]Airport `json:"airports"`
}

// Store gestisce la persistenza degli aeroporti per ogni chat Telegram.
type Store struct {
	mu             sync.RWMutex
	data           storeData
	path           string
	lastMetarTime  map[string]string // key: "chatID:ICAO" → last metar time repr
}

// NewStore carica o crea un nuovo store dal file specificato.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:          path,
		data:          storeData{Airports: make(map[int64][]Airport)},
		lastMetarTime: make(map[string]string),
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	if s.data.Airports == nil {
		s.data.Airports = make(map[int64][]Airport)
	}
	return s, nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// GetAirports restituisce gli aeroporti registrati per una chat.
func (s *Store) GetAirports(chatID int64) []Airport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.data.Airports[chatID]
	out := make([]Airport, len(src))
	copy(out, src)
	return out
}

// AddAirport aggiunge un aeroporto (evita duplicati). Restituisce false se già presente.
func (s *Store) AddAirport(chatID int64, airport Airport) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.data.Airports[chatID] {
		if a.ICAO == airport.ICAO {
			return false, nil
		}
	}
	s.data.Airports[chatID] = append(s.data.Airports[chatID], airport)
	return true, s.save()
}

// RemoveAirport rimuove un aeroporto. Restituisce false se non trovato.
func (s *Store) RemoveAirport(chatID int64, icao string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	airports := s.data.Airports[chatID]
	for i, a := range airports {
		if a.ICAO == icao {
			s.data.Airports[chatID] = append(airports[:i], airports[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

// AllChats restituisce una copia della mappa chatID → aeroporti.
func (s *Store) AllChats() map[int64][]Airport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int64][]Airport, len(s.data.Airports))
	for k, v := range s.data.Airports {
		cp := make([]Airport, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// IsNewMetar controlla se il time del METAR è diverso dall'ultimo noto.
// Se è nuovo, aggiorna il record e restituisce true.
func (s *Store) IsNewMetar(chatID int64, icao, timeRepr string) bool {
	key := fmt.Sprintf("%d:%s", chatID, icao)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastMetarTime[key] == timeRepr {
		return false
	}
	s.lastMetarTime[key] = timeRepr
	return true
}
