package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func setupTestServer() http.Handler {
	store = NewMemoryStore()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats)
	mux.HandleFunc("GET /campaigns/{campaignID}/events", handleGetEvents)
	return mux
}

// TestSeedEventsSurvives verifies the server successfully ingests seed/events.json,
// surviving all malformed records, retries, and conflicts.
func TestSeedEventsSurvives(t *testing.T) {
	handler := setupTestServer()

	seedData, err := os.ReadFile("seed/events.json")
	if err != nil {
		t.Fatalf("failed to read seed/events.json: %v", err)
	}

	req := httptest.NewRequest("POST", "/events", bytes.NewReader(seedData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result IngestionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Received != 235 {
		t.Errorf("expected 235 received events, got %d", result.Received)
	}
	if result.Accepted == 0 {
		t.Errorf("expected accepted > 0, got %d", result.Accepted)
	}
	if result.Rejected == 0 {
		t.Errorf("expected rejected > 0 due to known invalid records in seed, got %d", result.Rejected)
	}
	if result.Duplicates == 0 {
		t.Errorf("expected duplicates > 0 in seed data, got %d", result.Duplicates)
	}

	// Verify stats for cmp_summer_sale
	statsReq := httptest.NewRequest("GET", "/campaigns/cmp_summer_sale/stats", nil)
	statsRec := httptest.NewRecorder()
	handler.ServeHTTP(statsRec, statsReq)

	if statsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for campaign stats, got %d: %s", statsRec.Code, statsRec.Body.String())
	}

	var stats CampaignStats
	if err := json.Unmarshal(statsRec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}

	if stats.CampaignID != "cmp_summer_sale" {
		t.Errorf("expected cmp_summer_sale, got %s", stats.CampaignID)
	}
	if stats.Sent == 0 || stats.Delivered == 0 {
		t.Errorf("expected positive sent and delivered counts, got sent=%d, delivered=%d", stats.Sent, stats.Delivered)
	}
	if stats.DeliveryRate == nil || *stats.DeliveryRate <= 0 {
		t.Errorf("expected computed delivery rate")
	}
}

// TestBatchResilience verifies that a single bad event in a batch does not invalidate the other valid events.
func TestBatchResilience(t *testing.T) {
	handler := setupTestServer()

	batchPayload := `[
		{"event_id":"evt_good_1","campaign_id":"cmp_test","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
		{"event_id":"evt_bad_corrupt","campaign_id":"cmp_test","contact_id":"ct_2","type":"sent","timestamp":"not-a-timestamp"},
		{"event_id":"evt_good_2","campaign_id":"cmp_test","contact_id":"ct_3","type":"delivered","timestamp":"2026-08-10T06:05:00Z"}
	]`

	req := httptest.NewRequest("POST", "/events", bytes.NewReader([]byte(batchPayload)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var result IngestionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Received != 3 || result.Accepted != 2 || result.Rejected != 1 {
		t.Errorf("expected 3 received, 2 accepted, 1 rejected; got %+v", result)
	}
}

// TestIdempotentRetries ensures that resending the same payload does not inflate counts.
func TestIdempotentRetries(t *testing.T) {
	handler := setupTestServer()

	payload := `[{"event_id":"evt_dup_1","campaign_id":"cmp_idem","contact_id":"ct_1","type":"delivered","timestamp":"2026-08-10T06:00:00Z"}]`

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/events", bytes.NewReader([]byte(payload)))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var res IngestionResult
		_ = json.Unmarshal(rec.Body.Bytes(), &res)

		if i == 0 && res.Accepted != 1 {
			t.Errorf("first run should accept 1 event, got %d", res.Accepted)
		}
		if i > 0 && res.Duplicates != 1 {
			t.Errorf("retry %d should report 1 duplicate, got %d", i, res.Duplicates)
		}
	}

	statsReq := httptest.NewRequest("GET", "/campaigns/cmp_idem/stats", nil)
	statsRec := httptest.NewRecorder()
	handler.ServeHTTP(statsRec, statsReq)

	var stats CampaignStats
	_ = json.Unmarshal(statsRec.Body.Bytes(), &stats)
	if stats.Delivered != 1 {
		t.Fatalf("expected delivered count to remain 1 after retries, got %d", stats.Delivered)
	}
}

// TestConcurrentAccess tests simultaneous reads and writes under race detector.
func TestConcurrentAccess(t *testing.T) {
	handler := setupTestServer()

	var wg sync.WaitGroup
	workers := 10
	eventsPerWorker := 20

	for w := 0; w < workers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerWorker; i++ {
				ev := Event{
					EventID:    bytes.NewBufferString("evt_conc_").String(),
					CampaignID: "cmp_conc",
					ContactID:  "ct_conc",
					Type:       "sent",
					Timestamp:  "2026-08-10T06:00:00Z",
				}
				data, _ := json.Marshal([]Event{ev})
				req := httptest.NewRequest("POST", "/events", bytes.NewReader(data))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if i%5 == 0 {
					statsReq := httptest.NewRequest("GET", "/campaigns/cmp_conc/stats", nil)
					statsRec := httptest.NewRecorder()
					handler.ServeHTTP(statsRec, statsReq)
				}
			}
			_ = workerID
		}()
	}
	wg.Wait()
}
