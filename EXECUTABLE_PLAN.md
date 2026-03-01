# High-Performance Local Comic Web App — Executable Engineering Plan

## A) Architecture Decision

### Frontend: **SvelteKit (SPA-first) + Vite + TypeScript + Service Worker (PWA-lite)**

**Why this choice for performance (practical, not theoretical):**
- Svelte’s compiled reactivity has lower runtime overhead than typical VDOM-heavy setups for image-dense UIs.
- Fast startup with Vite dev/build pipeline and small JS footprint helps FMP/FCP targets.
- Easy progressive rendering patterns (route-level loading, skeleton-first, incremental list append).
- Mature virtualization options (`svelte-virtual` / custom windowing) for cover grid + long-strip reader.
- Responsive design without native wrapper complexity (Windows browser first, mobile browser second).

### Backend: **Go (Fiber or Chi) + SQLite + background worker pool**

**Why this choice for performance:**
- Go gives very low-latency HTTP serving and simple concurrency primitives for scan/thumbnail pipelines.
- Single static binary deployment is simple for local/NAS scenarios.
- Efficient stream handling for large image files and range/caching support.
- Background workers (bounded) avoid blocking request threads and enable progressive API responses.

### Storage
- **SQLite** for metadata/index/progress/task state.
- **Original files** remain on local disk/NAS (source of truth).
- **Thumbnail cache dir** on local disk (configurable max size + LRU cleanup).

### Runtime topology
1. Startup: load config, open SQLite, mount cache dir, warm minimal indexes.
2. UI opens instantly with skeleton + first paged library query.
3. Scan and thumbnail generation run as background tasks; UI subscribes/polls status.
4. Reader requests first page immediately, then prefetch queue handles nearby pages.

---

## B) Key Techniques Checklist (Performance-Critical)

### Progressive rendering
- [ ] Route shell renders immediately (no blocking data fetch for layout/chrome).
- [ ] Library page requests `page=1&limit=~60` only, append next pages on scroll.
- [ ] Manga detail renders metadata shell in ≤300ms, chapters load paged/incremental.
- [ ] Reader opens with page metadata first, requests only initial page image first.

### Thumbnail strategy
- [ ] Generate cover thumbnails server-side at scan time (target ≤50KB, WebP default).
- [ ] Use deterministic cache key from `(source path + mtime + size + preset)`.
- [ ] Optional chapter page thumbnails generated lazily (on first request / idle worker).
- [ ] Avoid serving originals for grid cards except explicit fallback.

### HTTP caching and transfer
- [ ] `Cache-Control: public, max-age=31536000, immutable` for content-addressed thumbs.
- [ ] `ETag` and `Last-Modified` on original/page responses.
- [ ] Conditional GET (`If-None-Match`, `If-Modified-Since`) support.
- [ ] Range request support for large files where browser benefits.
- [ ] `stale-while-revalidate` for API list endpoints where suitable.

### Reader performance
- [ ] DOM virtualization/windowing for long-strip mode (render viewport + overscan only).
- [ ] IntersectionObserver for lazy image activation.
- [ ] Bounded fetch queue (4–8 concurrent image requests).
- [ ] Direction-aware prefetch (N ahead, M behind; tune for scroll velocity).
- [ ] Retry with exponential backoff + jitter for transient file/NAS errors.
- [ ] Use `img.decoding="async"` and `fetchpriority`/priority hints for first image.
- [ ] Do not place blobs/binary buffers in global app state.

### Memory and decode control
- [ ] Drop offscreen images aggressively (unmount beyond overscan budget).
- [ ] Cap decoded image count in memory; rely on browser cache for revisit.
- [ ] Optional dynamic downscale endpoint for very large source pages on low-memory devices.

### Backend workload control
- [ ] Separate worker pools: scanning, thumbnailing, optional transcode.
- [ ] Bounded queues to avoid I/O storms on NAS.
- [ ] Debounced filesystem watch updates rather than rescanning entire library.

---

## C) Minimal Viable API List (Routes + Payload Outlines)

> All list endpoints must be paginated and return quickly from DB (never block on scan/thumbnail completion).

### 1) `GET /api/library`
**Query:** `page`, `limit`, `search`, `sort`, `order`, optional `tag`, `status`

**Response (example outline):**
```json
{
  "items": [
    {
      "id": "m_123",
      "title": "Example Manga",
      "coverThumbUrl": "/api/images/covers/m_123/thumb?p=sm",
      "chapterCount": 128,
      "updatedAt": "2026-02-18T10:00:00Z"
    }
  ],
  "page": 1,
  "limit": 60,
  "total": 5234,
  "hasMore": true
}
```

### 2) `GET /api/manga/{id}`
**Response:** lightweight metadata for instant detail shell
```json
{
  "id": "m_123",
  "title": "Example Manga",
  "description": "...",
  "authors": ["..."],
  "genres": ["..."],
  "coverThumbUrl": "/api/images/covers/m_123/thumb?p=md",
  "chapterCount": 128,
  "lastReadChapterId": "c_777"
}
```

### 3) `GET /api/manga/{id}/chapters`
**Query:** `page`, `limit`, `order=asc|desc`

**Response:**
```json
{
  "items": [
    {
      "id": "c_777",
      "mangaId": "m_123",
      "title": "Ch. 77",
      "number": 77,
      "pageCount": 38,
      "releaseDate": "2025-08-12T00:00:00Z"
    }
  ],
  "page": 1,
  "limit": 100,
  "total": 128,
  "hasMore": true
}
```

### 4) `GET /api/chapters/{id}/pages`
**Goal:** lightweight metadata only, sorted by reading order

```json
{
  "chapterId": "c_777",
  "pages": [
    {
      "index": 0,
      "width": 1600,
      "height": 2400,
      "mime": "image/webp",
      "sizeBytes": 5242880,
      "imageUrl": "/api/images/chapters/c_777/pages/0"
    }
  ]
}
```

### 5) `GET /api/images/covers/{id}/thumb`
**Query:** preset `p=sm|md|lg`, optional format `f=webp|avif`

**Behavior:**
- Serve cached thumbnail if exists.
- If missing: enqueue/perform fast generation path, return as soon as available.
- Strong cache headers + ETag.

### 6) `GET /api/images/chapters/{chapterId}/pages/{pageIndex}`
**Query:** optional width/quality/format for adaptive transcode (`w`, `q`, `f`).

**Behavior:**
- Default serves original file stream with conditional caching.
- Optional transformed variant cached by key.
- Support range/partial if needed by browser.

### 7) `POST /api/tasks/scan`
**Body:** optional roots override, mode (`full|incremental`).

**Response:**
```json
{ "taskId": "t_scan_001", "status": "queued" }
```

### 8) `GET /api/tasks/scan/status`
**Query:** optional `taskId`

**Response:**
```json
{
  "active": true,
  "taskId": "t_scan_001",
  "phase": "thumbnailing",
  "progress": { "scanned": 1240, "total": 5234 },
  "etaSec": 180
}
```

---

## D) Data Model Outline

### `manga`
- `id` (PK)
- `title`, `title_sort`
- `path` (unique)
- `cover_page_id` (nullable)
- `chapter_count`, `page_count`
- `created_at`, `updated_at`, `last_scan_at`

### `chapter`
- `id` (PK)
- `manga_id` (FK)
- `title`, `number`, `volume`
- `path` (unique)
- `page_count`
- `file_mtime`, `created_at`, `updated_at`

### `page`
- `id` (PK)
- `chapter_id` (FK)
- `page_index` (0-based)
- `path` (unique)
- `mime`, `width`, `height`, `size_bytes`
- `file_mtime`, `checksum` (optional fast hash)

### `progress`
- `id` (PK)
- `manga_id` (FK)
- `chapter_id` (FK)
- `page_index`
- `updated_at`

### `thumbnail_cache`
- `id` (PK)
- `source_type` (`cover|page`)
- `source_id`
- `preset` / transform params
- `format` (`webp|avif`)
- `cache_path` (unique)
- `byte_size`
- `source_mtime` / `cache_key`
- `last_access_at`, `created_at`

### `task`
- `id` (PK)
- `type` (`scan|thumb_backfill`)
- `status` (`queued|running|done|failed|canceled`)
- `payload_json`
- `progress_json`
- `started_at`, `finished_at`, `error`

**Indexes (minimum):**
- `manga(title_sort)`, `manga(updated_at)`
- `chapter(manga_id, number)`
- `page(chapter_id, page_index)`
- `thumbnail_cache(source_type, source_id, preset, format)`
- `task(type, status, started_at)`

---

## E) Milestones (Phased Plan)

### Phase 1 — MVP (fast-first UX baseline)
**Goal:** scan local folders, library grid, open reader and show first page quickly.

1. Backend skeleton (Go + SQLite): config, DB migrations, health endpoint.
2. Scanner: ingest manga/chapter/page metadata from configured roots.
3. Core APIs: `/api/library`, `/api/manga/{id}`, `/api/manga/{id}/chapters`, `/api/chapters/{id}/pages`, image page endpoint.
4. Frontend shell + routes: Library, Manga Detail, Reader.
5. Reader v1: fetch page metadata + request first page immediately; lazy load rest.
6. Basic persisted reading progress.

**Exit criteria:**
- First page displays without waiting for all chapter images.
- No endpoint blocks on full library rescan.

### Phase 2 — Performance pass (hard target alignment)

1. Thumbnail pipeline (cover first; optional page thumbs).
2. HTTP caching (ETag/Last-Modified/Cache-Control/304 paths).
3. Grid virtualization + reader virtualization/windowing.
4. Fetch queue with concurrency cap (default 6), direction-aware prefetch.
5. Retry/backoff logic and error placeholders.
6. NAS-friendly worker and I/O throttling; incremental scan mode.
7. Service worker for shell/static asset caching (not image blob hoarding).

**Exit criteria:**
- Library FMP ≤1s and first-screen covers ≤1.5s on warm-ish local setup.
- Reader first readable page ≤500ms after open in typical cases.
- Smooth long-strip scrolling with bounded DOM nodes.

### Phase 3 — UX pass (quality and retention)

1. Search/sort/filter UX with debounced queries.
2. Chapter jump, resume prompts, reading settings (prefetch depth, quality mode).
3. Mobile responsive tuning (touch targets, low-memory defaults).
4. Scan/task dashboard with progress and actionable errors.
5. Optional single-page mode (keyboard/tap navigation).

**Exit criteria:**
- Stable cross-device usability and no regression of perf budgets.

---

## F) Performance Validation Plan

### Metrics to track
- **Network/backend:** TTFB per API, p95 image endpoint latency, cache hit ratio, 304 rate.
- **Library UX:** route start → skeleton paint, first-screen covers loaded, FCP/LCP.
- **Manga detail UX:** navigation start → UI visible (target ≤300ms), chapters first batch latency.
- **Reader UX:** open → first readable page painted (target ≤500ms), subsequent page ready time.
- **Rendering:** scroll FPS, long tasks (>50ms), dropped frames.
- **Resource usage:** JS heap, decoded image memory pressure, DOM node count, CPU spikes.

### How to measure
1. **Browser DevTools Performance panel**
   - Record library load and reader scroll sessions.
   - Validate no large long tasks from rendering/image decode bursts.
2. **Lighthouse (desktop + mobile emulation)**
   - Track FCP/LCP/TBT trends on representative datasets.
3. **Custom in-app marks** (`performance.mark/measure`)
   - `nav_to_shell`, `nav_to_first_cover`, `reader_open_to_first_page`.
4. **Backend telemetry**
   - Per-route timing, queue depth, worker utilization, thumbnail generation time.
5. **Repeatable test script/profile**
   - Cold cache run, warm cache run, NAS-throttled run.
   - Dataset tiers: small (100 manga), medium (1000), large (5000+).

### Acceptance gate
- Performance budgets are CI-checked where possible (scripted smoke measurements).
- Any regression >15% on core metrics blocks release until explained/fixed.

---

## G) Risks / Tradeoffs

1. **AVIF vs WebP**
   - AVIF smaller files but slower encode/decode on some clients; WebP is safer default for latency.
   - Plan: WebP default thumbnails, AVIF optional preset for capable devices.

2. **SSR vs SPA**
   - SSR can improve first paint for remote apps, but local single-user app often bottlenecks on image I/O and JS hydration complexity.
   - Plan: SPA-first with ultra-light shell + aggressive caching; revisit SSR only if measured need.

3. **Cache size vs disk usage**
   - Large thumbnail/transcode cache improves speed but can grow quickly.
   - Plan: configurable max size + LRU eviction + cache stats UI.

4. **NAS latency variability**
   - Random I/O and network jitter can spike page/thumbnail load times.
   - Plan: bounded concurrency, prioritized first-visible content, retries, and local cache warming.

5. **Browser memory limits**
   - Long-strip with huge pages can trigger memory pressure/jank.
   - Plan: strict virtualization, limited overscan, adaptive quality/downscale option.

6. **Background tasks competing with reading**
   - Scan/thumb backfill can starve active reading if unthrottled.
   - Plan: task prioritization (reader > interactive API > background jobs).

---

## Initial Execution Backlog (First 2 Weeks)

### Week 1
- Define config schema (library roots, cache path, worker limits).
- Implement DB schema + migrations.
- Implement scan task and minimal metadata extraction.
- Ship paginated library API from DB.
- Build Library UI skeleton + incremental cards.

### Week 2
- Implement chapter/page metadata APIs.
- Implement reader with first-page-priority and lazy load queue.
- Add cover thumbnail generation + endpoint + caching headers.
- Add basic performance instrumentation and baseline measurements.

**Deliverable at end of Week 2:**
A usable local reader that opens quickly and progressively, with measured baseline metrics against the stated targets.
