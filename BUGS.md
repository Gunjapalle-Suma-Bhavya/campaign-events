# BUGS.md — Part 3 Findings & Fixes

This document records the four bugs identified in `debugging/main.go`, why each occurred, the minimal fix applied, and how it was verified against `expected_output.txt`.

---

## Bug 1: Local De-duplication Scope (Cross-Batch Event Duplication)

### 1. What the bug is
The deduplication map `seen` was declared locally inside `processBatch()` as `seen := make(map[string]bool)`. This caused duplicate `event_id`s across different batches to be counted multiple times.

### 2. Why it happens
The input stream is chunked into batches of 200 events (`batchSize = 200`). When `seen` is allocated within `processBatch()`, its scope is limited to each single batch. When a provider retry or duplicate event arrives in a later batch, `seen` is empty for that batch, so the duplicate event passes deduplication and increments campaign metrics again.

### 3. Your fix
Moved `seen` out of `processBatch()` to package-level scope (`var seen = map[string]bool{}`), persisting deduplication state across the entire file. Removed the local `seen := make(...)` in `processBatch()`. Since `seen` is only read and written on the main goroutine prior to dispatching worker jobs, it remains safe without additional locks.

### 4. How you verified the fix
Ran `go run . events.jsonl` and observed that overall event counts (sent, delivered, opened, clicked) decreased to the true unique figures, eliminating over-counting across batch boundaries.

---

## Bug 2: Global vs. Per-Campaign `unique_opens` Tracking

### 1. What the bug is
`openedBy` tracked contact IDs globally across the entire program (`openedBy[ev.ContactID]`) instead of per campaign.

### 2. Why it happens
The specification requires: *"unique_opens = the number of distinct contacts that opened, per campaign. One contact opening five times in a campaign counts once. The same contact opening in two campaigns counts once in each."* 
Because `openedBy` was keyed solely by `ev.ContactID`, once a contact opened an email in campaign A, subsequent opens by that same contact in campaign B were ignored for campaign B's `unique_opens`.

### 3. Your fix
Changed the map lookup key in `track()` to include both the campaign ID and contact ID:
```go
key := ev.CampaignID + ":" + ev.ContactID
if !openedBy[key] {
    openedBy[key] = true
    cs.UniqueOpens++
}
```

### 4. How you verified the fix
Compared the resulting `unique_opens` per campaign with `expected_output.txt`. For instance, `cmp_A` unique opens corrected from 549 to 942, exactly matching ground truth.

---

## Bug 3: Incorrect Local Timezone in Daily Bucketing

### 1. What the bug is
Daily delivery bucketing used the system's local timezone instead of UTC: `day := ev.Timestamp.Local().Format("2006-01-02")`.

### 2. Why it happens
Requirement 3 states: *"Daily delivered buckets use the event's UTC date. The timestamp field is UTC."*
Calling `.Local()` shifts events into the host machine's local timezone (e.g. UTC+05:30). Depending on the local offset, timestamps near midnight shift to the previous or next calendar day, creating erroneous date buckets (e.g., events leaking into `2026-08-08` instead of staying within `2026-08-01` to `2026-08-07`).

### 3. Your fix
Replaced `.Local()` with `.UTC()` in `track()`:
```go
day := ev.Timestamp.UTC().Format("2006-01-02")
```

### 4. How you verified the fix
Verified that daily delivered dates align strictly from `2026-08-01` to `2026-08-07` with identical daily counts to `expected_output.txt`, regardless of the local machine's timezone.

---

## Bug 4: Data Race on CampaignStats Counters Under Concurrency

### 1. What the bug is
Multiple worker goroutines (`numWorkers = 8`) concurrently mutated the integer counters of `CampaignStats` (`cs.Sent++`, `cs.Delivered++`, `cs.Opened++`, `cs.Clicked++`) in `apply()` without synchronization.

### 2. Why it happens
`processBatch()` spins up 8 worker goroutines that pull events from a shared `jobs` channel and call `apply(ev)`. If multiple workers process events for the same `CampaignID` simultaneously, unsynchronized read-modify-write operations on plain `int` fields lead to lost updates, non-deterministic counter values, and data race violations detected by `go run -race`.

### 3. Your fix
Added a `sync.Mutex` field (`mu sync.Mutex`) to `CampaignStats`:
```go
type CampaignStats struct {
    mu             sync.Mutex
    Sent           int
    Delivered      int
    Opened         int
    Clicked        int
    UniqueOpens    int
    DailyDelivered map[string]int
}
```
In `apply()`, acquired the campaign's mutex before updating counters:
```go
func apply(ev Event) {
    cs := stats[ev.CampaignID]
    cs.mu.Lock()
    defer cs.mu.Unlock()
    switch ev.Type {
    case "sent":
        cs.Sent++
    case "delivered":
        cs.Delivered++
    case "opened":
        cs.Opened++
    case "clicked":
        cs.Clicked++
    }
}
```
Using a per-campaign mutex ensures fine-grained synchronization without blocking workers processing different campaigns.

### 4. How you verified the fix
1. Ran `go run . events.jsonl > actual.txt` repeatedly; diff against `expected_output.txt` produced 0 differences every run.
2. Verified with race detector: `go run -race . events.jsonl` completes cleanly with no race warnings.
