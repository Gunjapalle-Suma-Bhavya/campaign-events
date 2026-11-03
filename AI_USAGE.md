# AI Usage Disclosure

## 1. Tools Used
- **Antigravity (Pair Programming Assistant powered by Gemini)**: Used throughout the assignment as an interactive pair-programming partner.
- **Go toolchain (v1.27.3)**: Built, tested, and benchmarked code natively.

---

## 2. Roughly What You Used Them For
- **Architectural Brainstorming**: Discussing the trade-offs between SQLite vs. pure in-memory standard library persistence, and element-by-element JSON decoding vs. full-slice unmarshaling.
- **Debugging Assistance (Part 3)**: Tracing the four bugs in `debugging/main.go` (cross-batch deduplication, contact ID scoping across campaigns, local vs. UTC timestamp formatting, and data race in worker goroutines).
- **Test Generation**: Crafting integration tests for batch resilience, seed data survival, idempotency, and race conditions.
- **Drafting Scaling & RCA Concepts (Parts 4 & 5)**: Structuring arguments for message queue bottlenecks, eventual consistency duplicates, and MTA greylisting/bot filter reversals.

---

## 3. One Suggestion from AI That You Rejected or Changed, and Why
- **Initial Suggestion**: The AI suggested using `modernc.org/sqlite` or an SQLite file to store events and compute campaign stats via SQL queries (`GROUP BY campaign_id`).
- **Why Rejected**: Adding external SQLite drivers introduces unnecessary module dependencies and compilation overhead, whereas the assignment specifically stated *"SQLite or in-memory persistence — your choice. No third-party dependencies are required."* We chose a sleek, clean in-memory repository with `sync.RWMutex` fine-grained locking. It executes instantaneously with zero external dependencies and runs identically on any environment.

---

## 4. One Thing AI Helped You Understand
- **Decoupled Batch Decoding**: AI highlighted the subtle vulnerability in the starter code: decoding the entire HTTP request directly into `[]Event` causes an entire batch of 200 items to abort if even one record has a bad field type (such as `"not-a-time"` in `seed/events.json`). Using `[]json.RawMessage` or element-by-element decoding ensures healthy events are accepted while faulty events are isolated and reported.

---

## 5. Anything AI Generated That You Then Had to Debug
- **Line Ending Discrepancies in Verification**: When verifying Part 3 fixes against `expected_output.txt`, initial PowerShell stdout redirects (`> actual.txt`) encoded the output in UTF-16 with CRLF line endings, causing file hash comparisons to fail despite identical data. We verified the text line-by-line and ensured exact byte-for-byte conformity with Unix LF ground truth.
