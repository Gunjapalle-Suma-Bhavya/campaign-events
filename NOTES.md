# NOTES.md — Campaign Events Engineering Review

This document contains Parts 1, 4, and 5 of the Campaign Events assignment, as well as reflections on trade-offs and future extensions.

---

## Part 1 — Read and Think First

### 1. Interpretation of the Problem
Relay operates as an intermediary between marketing clients and third-party delivery providers (email, SMS, push). As providers deliver messages and recipients interact, providers dispatch asynchronous webhooks to Relay containing event types: `sent`, `delivered`, `opened`, `clicked`.

Our responsibility is to build a high-performance, fault-tolerant service that:
1. Ingests event batches via `POST /events` without crashing or dropping valid records when providers send malformed records, retries, or out-of-order events.
2. Serves real-time aggregate statistics for marketers via `GET /campaigns/{campaign_id}/stats` without double counting.

### 2. Assumptions Made
- **Event ID Authority**: `event_id` is assigned by the provider and serves as the primary idempotency key. Receiving an identical `event_id` multiple times represents a provider retry and must not mutate counters or append duplicate logs.
- **Out-of-Order Lifecycle**: Events naturally arrive out of order (e.g. `opened` arriving prior to `delivered` due to provider queuing). We must not reject or synthesize synthetic precursor events.
- **Unique Attribution**: Marketing dashboards require tracking distinct contacts who engaged per campaign (`unique_opens` and `unique_clicks`), as well as raw interaction totals.
- **Standard Library Primacy**: To guarantee clean reproducibility across any OS, an in-memory thread-safe architecture using Go stdlib (`sync.RWMutex`) avoids external database installation or CGO dependencies.
- **Batch Fault-Tolerance**: If an array of 200 events contains 3 malformed records, the 197 valid events must be ingested; dropping the entire batch or failing with HTTP 400 punishes the provider and causes repeated retry loops.

### 3. Ambiguities Noticed in the Brief
- **Duplicate Definition**: Does "the same event" mean identical `event_id`, or identical `(campaign_id, contact_id, type)`? The seed data shows `evt_00164` appearing once as `sent` and once as `opened`. If `event_id` identifies the event as stated in the payload spec, the second event is a payload conflict. Our system respects `event_id` as unique and flags re-used IDs.
- **Definition of "How Many Opened"**: Does the PM want total raw open events or unique contacts who opened? Real-world marketers need both to compute open rates; our service tracks and returns both.
- **Missing or Partial Fields**: The spec marks `metadata` as optional, but leaves `contact_id` unspecified. In `seed/events.json`, record `evt_00214` has an empty `contact_id`. Without a contact, unique attribution is impossible, so we reject events lacking `contact_id` while allowing empty `metadata`.
- **Casing and Unknown Types**: Records in the seed data include `"OPENED"` (all caps) and `"spam_report"`. We normalize case and reject unknown types not specified in the contract.

### 4. Questions for the Product Manager
1. *"When providers retry a batch where 2 out of 100 events were rejected due to invalid payloads, do their systems support partial status responses (e.g. 200/207 with error breakdown), or do they expect all-or-nothing transactions?"*
2. *"Should the dashboard display raw opens, unique contacts who opened, or both? And is open rate calculated as unique opens over delivered or over sent?"*
3. *"Are campaigns expected to remain active indefinitely, or is there an archival/completion lifecycle after which late events should be flagged as expired?"*
4. *"What is the expected SLA for event-to-dashboard latency under peak promotional campaign blasts?"*

### 5. Prioritization Strategy
1. **Priority 1 (Ingestion Resilience)**: Build element-by-element JSON decoding so malformed events in `seed/events.json` do not fail valid items.
2. **Priority 2 (Deduplication & Concurrency)**: Implement thread-safe deduplication and fine-grained locking to prevent race conditions.
3. **Priority 3 (Dashboard API & Metrics)**: Expose clean, expressive metrics (`sent`, `delivered`, `opened`, `clicked`, `unique_opens`, `unique_clicks`, rates).
4. **Priority 4 (Verification & Debugging)**: Identify and resolve all 4 bugs in `debugging/` and prove byte-for-byte ground truth match.
5. **Priority 5 (Written Memos)**: Deeply address scaling to 100M events/day and investigative RCA for the marketer anomaly.

---

## Part 4 — Scale Memo (100 Million Events / Day)

### 1. What Breaks First in the Current Implementation?
- **Heap Memory Exhaustion (OOM)**: At 100M events/day, storing raw event objects, deduplication sets, and contact sets in Go memory will consume dozens of gigabytes within hours.
- **Lock Contention**: Under high ingress (~1,200 to 10,000+ events/sec peak), mutexes guarding in-memory state become CPU bottlenecks, degrading HTTP response latency.
- **Zero Durability on Crash**: An unhandled panic or instance restart wipes all campaign counters.

### 2. Changes and Rollout Order
1. **Edge Buffer / Ingestion Queue**: Place an asynchronous message queue (Apache Kafka, AWS Kinesis, or GCP PubSub) behind the webhook endpoint.
2. **Distributed Deduplication Layer**: Use a clustered Redis/Aerospike cluster with time-to-live (e.g. 7-day TTL on `event_id`) to deduplicate incoming events at the consumer layer.
3. **Stream Aggregation**: Deploy stream workers (e.g., Apache Flink or Kafka Streams) to maintain running campaign counters in real-time.
4. **Analytical Columnar Storage**: Stream raw events to ClickHouse or BigQuery for historical reporting and cold storage.

### 3. API Contract and Storage Design Changes
- **API Contract**: The `POST /events` endpoint changes from synchronous processing to an asynchronous acknowledgment (`202 Accepted` with a batch ingestion ID).
- **Storage Design**: Transition from in-memory maps to:
  - Redis for fast O(1) counter reads and HyperLogLog structures for unique open/click estimation.
  - Partitioned time-series tables (ClickHouse / TimescaleDB) for event querying.

### 4. Queue Introduction and New Problems Created
- **Why Queue**: Smooths traffic spikes, prevents provider timeouts, and isolates ingestion from processing outages.
- **New Problems**:
  - Eventual consistency: A slight propagation delay between provider delivery and dashboard counter reflection.
  - Operational overhead: Monitoring queue consumer lag, partition balancing, and broker health.
  - At-least-once re-deliveries: Network hiccups cause consumers to re-read messages.

### 5. Where Duplicates Can Now Sneak In
- **Network Retries**: Between webhook ingestion and message queue publish.
- **Consumer Crashes**: A consumer updates Redis counters, crashes before committing the queue offset, and the new consumer replays the message batch.
- **Concurrent Partitions**: Two duplicate events assigned to different queue partitions processed simultaneously by different worker nodes.

### 6. Keeping Counters Trustworthy
- **Idempotency Stores**: Consumers check and set an idempotency key before applying increments.
- **HyperLogLog for Uniques**: Using Redis `PFADD` for unique contacts guarantees that even if a message is processed three times, the unique count remains unchanged.
- **Daily Reconciliation**: A scheduled nightly batch reconciliation job replays raw events from immutable object storage (S3 parquet files) and updates counters to correct any transient drift.

### 7. What to Monitor
- **Ingestion Edge**: Request throughput (req/s), HTTP status codes (2xx, 4xx, 5xx), p95/p99 latency.
- **Queue Health**: Consumer lag (unprocessed events), ingestion queue depth.
- **Data Quality**: Percentage of rejected malformed events, duplicate rate per provider.
- **System Metrics**: Worker memory/CPU usage, Redis hit rates, database write latencies.

### 8. What Deliberately NOT to Solve Yet
- **Multi-Region Active-Active Replication**: Adds complex conflict resolution and distributed locking; a single-region multi-AZ deployment easily handles 100M/day.
- **Complex Ad-Hoc Analytics**: Marketers need predefined campaign counters, not arbitrary multi-table joins. Avoid heavy OLAP query complexity initially.

---

## Part 5 — The Angry Marketer Investigation

### 1. Plausible Explanations (Before Assuming Code is Broken)
1. **Automated Security Scanner / Bot Filter Reversals**: Major email security providers (e.g. Proofpoint, Mimecast, Apple Mail Privacy Protection) pre-fetch emails and trigger false open webhooks. Many message providers run retroactive bot-detection heuristics and subsequently issue counter-adjustments or negative adjustments to purge fraudulent open counts.
2. **Dashboard Query Filter / Timezone Windowing**: If the dashboard default view is "Opens in the last 24 hours" or a sliding rolling window, opens recorded at 09:55 yesterday may have expired by 10:30 today, lowering the visible aggregate.
3. **Contact Deduplication or Suppression Merge**: If a list cleanup or contact deduplication job ran at 10:15 (merging duplicated subscriber profiles or applying GDPR suppression requests), previous duplicate opens by the merged contact were consolidated into a single unique open.
4. **Cache Invalidation / Node Consistency Drift**: At 10:00, the user may have read from a stale cache or unsynchronized replica with an un-deduplicated count; at 10:30, a cache eviction refreshed the count to the true distinct figure.

### 2. Is the Delivered Number Rising Actually Suspicious?
- **No, it is completely normal.** Delivery providers do not deliver 1,000,000 emails instantaneously. Mail transfer agents (MTAs) experience SMTP greylisting, rate limits, and transient deferrals. Providers retry deferred messages with exponential backoff over hours. Seeing delivered count increase by 5,000 between 10:00 and 10:30 is standard delivery provider behavior.

### 3. Investigation Protocol
1. **Inspect Webhook Audit Logs**: Search provider event payloads between 09:50 and 10:30 for campaign ID to see if adjustment events, retractions, or bot flags were transmitted.
2. **Execute Database Ground Truth Query**:
   ```sql
   SELECT 
       COUNT(*) AS total_opens, 
       COUNT(DISTINCT contact_id) AS unique_opens 
   FROM events 
   WHERE campaign_id = 'cmp_123' AND type = 'opened';
   ```
   Determine if the true event record count was 248,000 all along (confirming an overcounting or caching glitch at 10:00) or if open events were deleted/retracted.
3. **Check System Release & Job Logs**: Verify if a cache warming script, background reconciliation worker, or code deployment occurred at ~10:15.

---

## What I Completed / What I Intentionally Skipped

### Completed:
- **Resilient Batch Ingestion**: Element-by-element JSON stream decoding that survives dirty provider traffic (`seed/events.json`).
- **Idempotent In-Memory Repository**: Concurrent safe deduplication, counter updates, and distinct contact tracking.
- **Dashboard API**: Detailed stats endpoint returning raw counters, unique interaction figures, delivery rate, open rate, and click-through rate.
- **Paginated Event Activity**: Added optional `GET /campaigns/{campaign_id}/events` endpoint.
- **Part 3 Debugging**: Identified, explained, and fixed all 4 bugs in `debugging/main.go`, achieving a 100% byte-for-byte ground truth match against `expected_output.txt`.
- **Test Suite**: Automated tests covering seed data survival, batch error isolation, retry idempotency, and concurrency safety.

### Intentionally Skipped:
- **Heavy Database Setup**: Opted for Go in-memory store over SQLite to guarantee zero-dependency execution and zero CGO issues on any environment.
- **Authentication & Authorization**: Left open per instructions to focus on core domain resilience.
- **Frontend UI Polish**: Omitted UI components to prioritize backend correctness, tests, and architectural memos.

---

## If I Had Another Day

1. **Persistent Storage Engine**: Add an SQLite/PostgreSQL storage adapter implementing the same `Store` interface with database migration scripts.
2. **Prometheus Metrics & Structured Logging**: Export Prometheus gauges and counters (`events_received_total`, `events_rejected_total`, `ingest_duration_seconds`) and zap/zerolog structured JSON logging.
3. **Configurable Normalization Rules**: Provide per-provider normalization plugins (e.g. mapping provider-specific statuses like `"bounced"` or `"unsubscribed"`).
4. **Dead Letter Queue (DLQ)**: Persist rejected malformed events to a dead-letter repository so engineers can inspect and replay provider anomalies.
