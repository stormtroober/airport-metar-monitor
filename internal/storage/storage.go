package storage

import (
"encoding/json"
"fmt"
"os"
"sync"
)

// Airport represents an airport registered by a user.
type Airport struct {
	ICAO string `json:"icao"`
	Name string `json:"name"`
	City string `json:"city"`
}

type storeData struct {
	Airports map[int64][]Airport `json:"airports"`
}

// Store manages the persistence of airports for each Telegram chat.
type Store struct {
	mu            sync.RWMutex
	data          storeData
	path          string
	lastMetarTime map[string]string // key: "chatID:ICAO" → last metar time repr
}

// NewStore loads or creates a new store from the specified file.
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

// GetAirports returns the registered airports for a chat.
func (s *Store) GetAirports(chatID int64) []Airport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.data.Airports[chatID]
	out := make([]Airport, len(src))
	copy(out, src)
	return out
}

// AddAirport adds an airport (prevents duplicates). Returns false if already present.
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

// RemoveAirport removes an airport. Returns false if not found.
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

// AllChats returns a copy of the chatID -> airports map.
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

// IsNewMetar checks if the METAR time is different from the last known one.
// If it is new, it updates the record and returns true.
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
