# Campaign Events Service & Engineering Assignment

A high-performance, fault-tolerant Go service for ingesting marketing event webhooks and serving campaign statistics, alongside solutions to the debugging exercise and architecture memos.

---

## 📁 Repository Overview

```
├── NOTES.md          # Parts 1, 4, 5: Assumptions, 100M/day Scale Memo, and Angry Marketer RCA
├── BUGS.md           # Part 3: Detailed findings, root causes, and fixes for the 4 debugging bugs
├── AI_USAGE.md       # AI usage disclosure and reflections
├── starter/          # Part 2: The Campaign Events API service implementation & tests
│   ├── main.go       # HTTP server, routing, and endpoint handlers
│   ├── store.go      # Thread-safe in-memory store and event validation
│   ├── types.go      # Domain models and response payloads
│   ├── service_test.go # Comprehensive unit and integration test suite
│   ├── README.md     # In-depth API documentation and curl examples
│   └── seed/         # seed/events.json (realistic messy provider traffic)
└── debugging/        # Part 3: Debugged & fixed campaign stats CLI tool
    ├── main.go       # Fixed program (100% match with expected_output.txt)
    ├── events.jsonl  # 20,000 line sample event stream
    ├── expected_output.txt # Ground truth output
    └── README.md     # Exercise specification
```

---

## 🚀 Quick Start

### 1. Run the Campaign Events Service (Part 2)
```bash
cd starter
go run .
```
The service listens on `:8080`.

#### Ingest Events (`POST /events`):
```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  --data-binary @seed/events.json
```

#### Query Campaign Stats (`GET /campaigns/{campaign_id}/stats`):
```bash
curl http://localhost:8080/campaigns/cmp_summer_sale/stats
```

#### Run Service Tests:
```bash
cd starter
go test -v ./...
```

---

### 2. Run the Debugging Tool (Part 3)
```bash
cd debugging
go run . events.jsonl
```
This program output matches `expected_output.txt` byte-for-byte with zero data race warnings under `go run -race . events.jsonl`.

---

## 📌 Deliverable Documentation Links
- **Architectural & Design Notes**: [NOTES.md](NOTES.md)
- **Bug Analysis & Fix Verification**: [BUGS.md](BUGS.md)
- **AI Tooling Disclosure**: [AI_USAGE.md](AI_USAGE.md)
- **Service API Documentation**: [starter/README.md](starter/README.md)
