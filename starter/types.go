package main

import "time"

// Event represents one webhook delivery or interaction event from a message provider.
type Event struct {
	EventID    string            `json:"event_id"`
	CampaignID string            `json:"campaign_id"`
	ContactID  string            `json:"contact_id"`
	Type       string            `json:"type"` // sent | delivered | opened | clicked
	Timestamp  string            `json:"timestamp"`
	Metadata   map[string]string `json:"metadata,omitempty"`

	// ParsedTime holds the normalized UTC timestamp after validation
	ParsedTime time.Time `json:"-"`
}

// IngestionResult summarizes the outcome of a batch ingestion request.
type IngestionResult struct {
	Received   int             `json:"received"`
	Accepted   int             `json:"accepted"`
	Duplicates int             `json:"duplicates"`
	Rejected   int             `json:"rejected"`
	Errors     []RejectedEvent `json:"errors,omitempty"`
}

// RejectedEvent details an event that failed validation.
type RejectedEvent struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}

// CampaignStats provides dashboard metrics for a campaign.
type CampaignStats struct {
	CampaignID       string   `json:"campaign_id"`
	Sent             int      `json:"sent"`
	Delivered        int      `json:"delivered"`
	Opened           int      `json:"opened"`
	Clicked          int      `json:"clicked"`
	UniqueOpens      int      `json:"unique_opens"`
	UniqueClicks     int      `json:"unique_clicks"`
	DeliveryRate     *float64 `json:"delivery_rate,omitempty"`      // delivered / sent
	OpenRate         *float64 `json:"open_rate,omitempty"`          // unique_opens / delivered
	ClickThroughRate *float64 `json:"click_through_rate,omitempty"` // unique_clicks / delivered
	FirstEventAt     *string  `json:"first_event_at,omitempty"`
	LastEventAt      *string  `json:"last_event_at,omitempty"`
}

// PaginatedEventsResponse provides a paginated list of campaign events.
type PaginatedEventsResponse struct {
	CampaignID string  `json:"campaign_id"`
	Total      int     `json:"total"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
	Events     []Event `json:"events"`
}
