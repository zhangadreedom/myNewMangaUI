# Online Source Design

This document records the outcomes of Task 1, Task 2, and Task 3 for adding
online manga support without changing the current reader-heavy UI model.

## Task 1: Current Project Extension Points

The current project already has a clean split that can support online sources:

- `cmd/server/main.go`
  - constructs config, DB, scanner, image service, and API router
- `internal/api`
  - exposes REST endpoints used by the single-page frontend
- `internal/scan`
  - imports local filesystem manga into SQLite
- `internal/image`
  - creates cover thumbnails and serves cached image derivatives
- `internal/media`
  - reads file and archive-backed image refs
- `internal/api/static/app.js`
  - drives three core UX modes: library, manga detail, and reader

### Recommended insertion points

1. Add a new `internal/online` package
   - source adapter contracts
   - source-specific implementation for `18comic`
   - session handling
   - metadata cache helpers

2. Add a new `internal/download` package
   - persistent job queue
   - chapter/page work items
   - resume, retry, pause, cancel
   - conversion into local-library-compatible layout

3. Extend `internal/api`
   - `/api/online/...` for source-backed search/detail/chapter/page metadata
   - `/api/online/.../image/...` for proxied image delivery
   - `/api/tasks/downloads/...` for job lifecycle and progress

4. Keep `internal/media` focused on local file and archive media
   - do not overload it with remote-site HTTP concerns
   - remote image proxying should live in `internal/online` or `internal/download`

5. Reuse the existing reader frontend
   - current reader only needs `{ chapter, pages[], imageUrl }`
   - online pages can be exposed through new backend image proxy URLs

## Task 2: JMComic-qt Reference Mapping

Reference project:
- `tonquer/JMComic-qt`
- README states it is a Python + Qt desktop client
- README also states support for browsing, reading, and downloading
- README credits `hect0x7/JMComic-Crawler-Python`

Practical takeaway: treat JMComic-qt as a protocol and workflow reference, not
as code to embed directly.

### What is worth borrowing conceptually

1. Request/session layer
   - login/session persistence
   - source-specific headers/cookies/user agent handling
   - retry and fallback around unstable upstream behavior

2. Metadata fetch flow
   - search
   - manga detail
   - chapter listing
   - page/image resolution

3. Decode pipeline
   - source-specific image/page decoding should stay server-side
   - the browser should never know upstream cookies or decode rules

4. Download workflow
   - queued background tasks
   - progress tracking
   - partial failure recovery
   - resumable per-page execution

### What should not be copied structurally

1. Qt UI and view state
2. Desktop-specific task orchestration
3. App settings and packaging concerns unrelated to the web app

### Mapping from JMComic-style concepts to this project

- JMComic request/session logic
  -> `internal/online/provider_18comic.go`

- JMComic metadata adapters
  -> `internal/online/service.go`

- JMComic download tasks
  -> `internal/download/service.go`

- JMComic decoded image access
  -> `internal/api/online.go` + backend image proxy/cache

## Task 3: Data Model, Config, and Storage Layout

### New config surface

Add an `online` section:

- `enabled`
- `cachePath`
- `downloadsPath`
- `requestTimeoutSeconds`
- `imageProxyTTLSeconds`
- `sources[]`

Each source should support:

- `id`
- `name`
- `baseURL`
- `enabled`
- optional credentials/cookie/session material
- `userAgent`
- `maxConcurrentRequests`
- `requestIntervalMs`

### New SQLite entities

1. `source`
   - installed/enabled online sources

2. `online_manga`
   - cached remote manga metadata keyed by `(source_id, external_id)`

3. `online_chapter`
   - cached remote chapter metadata keyed by `(source_id, external_chapter_id)`

4. `download_job`
   - top-level persistent download task

5. `download_job_chapter`
   - chapter-level progress rows per job

6. `download_item`
   - page-level work item rows for retry/resume

### Filesystem layout

Temporary online cache:

`cache/online/{source-id}/...`

Persistent downloads:

`data/downloads/{source-id}/{manga-id}/`

Suggested finished structure:

```text
data/downloads/18comic/{manga-id}/
  metadata.json
  cover.jpg
  chapters/
    001-{chapter-id}/
      001.jpg
      002.jpg
```

### API shape to target in later tasks

```text
GET  /api/online/sources
GET  /api/online/{sourceID}/search?q=
GET  /api/online/{sourceID}/manga/{mangaID}
GET  /api/online/{sourceID}/manga/{mangaID}/chapters
GET  /api/online/{sourceID}/chapters/{chapterID}/pages
GET  /api/online/{sourceID}/image/{encodedRef}

POST /api/tasks/downloads
GET  /api/tasks/downloads
GET  /api/tasks/downloads/{jobID}
POST /api/tasks/downloads/{jobID}/pause
POST /api/tasks/downloads/{jobID}/resume
POST /api/tasks/downloads/{jobID}/cancel
POST /api/tasks/downloads/{jobID}/retry
```

### Reader route strategy

Do not overload local IDs with remote IDs.

Recommended new frontend route family:

```text
#/online/{sourceID}
#/online/{sourceID}/manga/{mangaID}
#/online/{sourceID}/chapter/{chapterID}
```

This keeps local and remote reading contexts separate while reusing almost all
reader rendering logic.

## Task 8-12: Delivery Notes

The initial HTML-scraping approach was kept only as a fallback. The primary
`18comic` implementation now uses a mobile-style API provider modeled after the
JMComic protocol flow.

### Mobile provider notes

- primary API paths:
  - `/search`
  - `/album`
  - `/chapter`
  - `/chapter_view_template`
- request signing follows the JM-style token pattern
- API payloads are decoded server-side
- chapter images are fetched and unscrambled server-side before reaching the
  browser

### Runtime requirements

- `18comic` requests are expected to go through a local proxy
  - current local default: `http://127.0.0.1:10087`
- the browser UI never talks to `18comic` directly
- downloaded manga is stored under `data/downloads/{sourceID}/...`
- the scanner now mounts those per-source download directories as bookshelves

### Task status summary

- Task 8: online source UI now reads from the mobile API-backed provider
- Task 9: single-chapter download is wired and verified
- Task 10: persistent download job APIs support pause, resume, cancel, and retry
- Task 11: completed downloads are rescanned into the local bookshelf model
- Task 12: test/build verification and design notes have been updated alongside
  the implementation
