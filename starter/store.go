package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store defines the interface for persisting and querying campaign events.
type Store interface {
	RecordEvent(ev Event) (accepted bool, duplicate bool, err error)
	GetCampaignStats(campaignID string) (*CampaignStats, error)
	GetCampaignEvents(campaignID string, limit, offset int) (*PaginatedEventsResponse, error)
}

// MemoryStore is a thread-safe in-memory implementation of Store.
type MemoryStore struct {
	mu        sync.RWMutex
	seenIDs   map[string]struct{}
	campaigns map[string]*campaignData
}

type campaignData struct {
	mu              sync.RWMutex
	sent            int
	delivered       int
	opened          int
	clicked         int
	openedContacts  map[string]struct{}
	clickedContacts map[string]struct{}
	firstEventTime  time.Time
	lastEventTime   time.Time
	events          []Event
}

// NewMemoryStore initializes a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seenIDs:   make(map[string]struct{}),
		campaigns: make(map[string]*campaignData),
	}
}

// RecordEvent persists an event if valid and non-duplicate.
func (s *MemoryStore) RecordEvent(ev Event) (accepted bool, duplicate bool, err error) {
	s.mu.Lock()
	if _, exists := s.seenIDs[ev.EventID]; exists {
		s.mu.Unlock()
		return false, true, nil
	}
	s.seenIDs[ev.EventID] = struct{}{}

	cd, exists := s.campaigns[ev.CampaignID]
	if !exists {
		cd = &campaignData{
			openedContacts:  make(map[string]struct{}),
			clickedContacts: make(map[string]struct{}),
			events:          make([]Event, 0),
		}
		s.campaigns[ev.CampaignID] = cd
	}
	s.mu.Unlock()

	cd.mu.Lock()
	defer cd.mu.Unlock()

	switch ev.Type {
	case "sent":
		cd.sent++
	case "delivered":
		cd.delivered++
	case "opened":
		cd.opened++
		if ev.ContactID != "" {
			cd.openedContacts[ev.ContactID] = struct{}{}
		}
	case "clicked":
		cd.clicked++
		if ev.ContactID != "" {
			cd.clickedContacts[ev.ContactID] = struct{}{}
		}
	}

	if cd.firstEventTime.IsZero() || ev.ParsedTime.Before(cd.firstEventTime) {
		cd.firstEventTime = ev.ParsedTime
	}
	if cd.lastEventTime.IsZero() || ev.ParsedTime.After(cd.lastEventTime) {
		cd.lastEventTime = ev.ParsedTime
	}

	cd.events = append(cd.events, ev)
	return true, false, nil
}

// GetCampaignStats computes and returns statistics for a given campaign.
func (s *MemoryStore) GetCampaignStats(campaignID string) (*CampaignStats, error) {
	s.mu.RLock()
	cd, exists := s.campaigns[campaignID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("campaign %q not found", campaignID)
	}

	cd.mu.RLock()
	defer cd.mu.RUnlock()

	uniqueOpens := len(cd.openedContacts)
	uniqueClicks := len(cd.clickedContacts)

	stats := &CampaignStats{
		CampaignID:   campaignID,
		Sent:         cd.sent,
		Delivered:    cd.delivered,
		Opened:       cd.opened,
		Clicked:      cd.clicked,
		UniqueOpens:  uniqueOpens,
		UniqueClicks: uniqueClicks,
	}

	if cd.sent > 0 {
		rate := roundRate(float64(cd.delivered) / float64(cd.sent))
		stats.DeliveryRate = &rate
	}
	if cd.delivered > 0 {
		openRate := roundRate(float64(uniqueOpens) / float64(cd.delivered))
		stats.OpenRate = &openRate

		ctr := roundRate(float64(uniqueClicks) / float64(cd.delivered))
		stats.ClickThroughRate = &ctr
	}

	if !cd.firstEventTime.IsZero() {
		t := cd.firstEventTime.UTC().Format(time.RFC3339)
		stats.FirstEventAt = &t
	}
	if !cd.lastEventTime.IsZero() {
		t := cd.lastEventTime.UTC().Format(time.RFC3339)
		stats.LastEventAt = &t
	}

	return stats, nil
}

// GetCampaignEvents returns paginated events for a campaign sorted newest-first.
func (s *MemoryStore) GetCampaignEvents(campaignID string, limit, offset int) (*PaginatedEventsResponse, error) {
	s.mu.RLock()
	cd, exists := s.campaigns[campaignID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("campaign %q not found", campaignID)
	}

	cd.mu.RLock()
	defer cd.mu.RUnlock()

	total := len(cd.events)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Copy events and sort newest first
	sortedEvents := make([]Event, len(cd.events))
	copy(sortedEvents, cd.events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].ParsedTime.After(sortedEvents[j].ParsedTime)
	})

	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	page := sortedEvents[start:end]
	return &PaginatedEventsResponse{
		CampaignID: campaignID,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		Events:     page,
	}, nil
}

func roundRate(val float64) float64 {
	return math.Round(val*10000) / 10000
}

// NormalizeAndValidate checks and parses an event.
func NormalizeAndValidate(raw Event) (Event, error) {
	ev := raw
	ev.EventID = strings.TrimSpace(ev.EventID)
	ev.CampaignID = strings.TrimSpace(ev.CampaignID)
	ev.ContactID = strings.TrimSpace(ev.ContactID)
	ev.Type = strings.ToLower(strings.TrimSpace(ev.Type))
	ev.Timestamp = strings.TrimSpace(ev.Timestamp)

	if ev.EventID == "" {
		return ev, fmt.Errorf("missing or empty event_id")
	}
	if ev.CampaignID == "" {
		return ev, fmt.Errorf("missing or empty campaign_id")
	}
	if ev.ContactID == "" {
		return ev, fmt.Errorf("missing or empty contact_id")
	}

	switch ev.Type {
	case "sent", "delivered", "opened", "clicked":
		// valid
	default:
		return ev, fmt.Errorf("unsupported event type %q (must be sent, delivered, opened, or clicked)", ev.Type)
	}

	if ev.Timestamp == "" {
		return ev, fmt.Errorf("missing timestamp")
	}

	t, err := parseTimestamp(ev.Timestamp)
	if err != nil {
		return ev, fmt.Errorf("invalid timestamp format %q: %w", ev.Timestamp, err)
	}
	ev.ParsedTime = t.UTC()

	return ev, nil
}

// Supported timestamp formats in provider ecosystems
var supportedTimeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"02-01-2006 15:04",
	"10-08-2006 15:04",
}

func parseTimestamp(ts string) (time.Time, error) {
	for _, layout := range supportedTimeFormats {
		if t, err := time.Parse(layout, ts); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date")
}
