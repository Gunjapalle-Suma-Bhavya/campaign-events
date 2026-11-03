# Campaign Events Service

A high-performance, resilient event ingestion and metrics service built in Go (standard library only, zero external dependencies).

## Key Features

1. **Resilient Batch Ingestion (`POST /events`)**:
   - Decodes requests using `json.RawMessage` element-by-element so that malformed events (bad timestamps, missing IDs, unknown types) do not fail the entire batch.
   - Idempotently de-duplicates provider retries based on `event_id`.
   - Normalizes event types (e.g., handles casing like `"OPENED"`).
   - Returns a structured 200 OK summary detailing accepted, duplicate, and rejected event counts along with specific validation reasons.

2. **Campaign Stats API (`GET /campaigns/{campaign_id}/stats`)**:
   - Serves real-time numbers for marketers: total `sent`, `delivered`, `opened`, `clicked`.
   - Tracks `unique_opens` and `unique_clicks` (distinct contacts).
   - Computes delivery rate (`delivered / sent`), open rate (`unique_opens / delivered`), and click-through rate (`unique_clicks / delivered`).
   - Tracks campaign time boundaries (`first_event_at`, `last_event_at`).

3. **Event Activity Stream (`GET /campaigns/{campaign_id}/events`)**:
   - Optional paginated query endpoint (`limit`, `offset`) sorted newest-first for real-time dashboard activity feeds.

4. **Thread-Safe In-Memory Persistence**:
   - Backed by an in-memory store utilizing fine-grained `sync.RWMutex` locks per campaign, ensuring maximum concurrency and zero database setup overhead.

---

## Requirements

- Go 1.22 or later.

---

## How to Run

From the `starter/` directory:

```bash
go run .
```

The server listens on `:8080`.

---

## Running the Tests

To run the unit and integration tests (including the seed survival test):

```bash
go test -v ./...
```

---

## API Examples

### 1. Ingest Events (`POST /events`)

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '[
    {
      "event_id": "evt_001",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_100",
      "type": "sent",
      "timestamp": "2026-08-10T08:00:00Z"
    },
    {
      "event_id": "evt_002",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_100",
      "type": "delivered",
      "timestamp": "2026-08-10T08:01:00Z"
    }
  ]'
```

**Response:**
```json
{
  "received": 2,
  "accepted": 2,
  "duplicates": 0,
  "rejected": 0
}
```

### 2. Loading Real-World Seed Data

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  --data-binary @seed/events.json
```

**Response:**
```json
{
  "received": 235,
  "accepted": 218,
  "duplicates": 13,
  "rejected": 4,
  "errors": [
    {"index": 27, "event_id": "evt_00212", "reason": "invalid timestamp format \"not-a-time\""},
    {"index": 80, "event_id": "", "reason": "missing or empty event_id"},
    {"index": 116, "event_id": "evt_00214", "reason": "missing or empty contact_id"},
    {"index": 122, "event_id": "evt_00215", "reason": "unsupported event type \"spam_report\" (must be sent, delivered, opened, or clicked)"}
  ]
}
```

### 3. Fetch Campaign Statistics (`GET /campaigns/{campaign_id}/stats`)

```bash
curl http://localhost:8080/campaigns/cmp_summer_sale/stats
```

**Response:**
```json
{
  "campaign_id": "cmp_summer_sale",
  "sent": 38,
  "delivered": 34,
  "opened": 24,
  "clicked": 10,
  "unique_opens": 22,
  "unique_clicks": 9,
  "delivery_rate": 0.8947,
  "open_rate": 0.6471,
  "click_through_rate": 0.2647,
  "first_event_at": "2026-08-10T06:01:00Z",
  "last_event_at": "2026-08-10T18:28:00Z"
}
```

### 4. Fetch Paginated Events (`GET /campaigns/{campaign_id}/events`)

```bash
curl "http://localhost:8080/campaigns/cmp_summer_sale/events?limit=5&offset=0"
```
