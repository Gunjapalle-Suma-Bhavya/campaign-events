package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

var store Store

func main() {
	store = NewMemoryStore()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats)
	mux.HandleFunc("GET /campaigns/{campaignID}/events", handleGetEvents)

	addr := ":8080"
	fmt.Printf("listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// handlePostEvents ingests a JSON array of events from message providers.
// It uses element-level decoding so that a malformed event does not fail the whole batch.
func handlePostEvents(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
		return
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(body, &rawItems); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be a JSON array of events"})
		return
	}

	result := IngestionResult{
		Received: len(rawItems),
		Errors:   make([]RejectedEvent, 0),
	}

	for i, rawMsg := range rawItems {
		var raw Event
		if err := json.Unmarshal(rawMsg, &raw); err != nil {
			result.Rejected++
			result.Errors = append(result.Errors, RejectedEvent{
				Index:  i,
				Reason: fmt.Sprintf("malformed json: %v", err),
			})
			continue
		}

		validEvent, err := NormalizeAndValidate(raw)
		if err != nil {
			result.Rejected++
			result.Errors = append(result.Errors, RejectedEvent{
				Index:   i,
				EventID: raw.EventID,
				Reason:  err.Error(),
			})
			continue
		}

		accepted, duplicate, err := store.RecordEvent(validEvent)
		if err != nil {
			result.Rejected++
			result.Errors = append(result.Errors, RejectedEvent{
				Index:   i,
				EventID: validEvent.EventID,
				Reason:  err.Error(),
			})
			continue
		}

		if duplicate {
			result.Duplicates++
		} else if accepted {
			result.Accepted++
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGetStats returns aggregated statistics for one campaign.
func handleGetStats(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")
	if campaignID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campaignID is required"})
		return
	}

	stats, err := store.GetCampaignStats(campaignID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleGetEvents returns a paginated list of events for a campaign.
func handleGetEvents(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")
	if campaignID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campaignID is required"})
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	eventsResp, err := store.GetCampaignEvents(campaignID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, eventsResp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
