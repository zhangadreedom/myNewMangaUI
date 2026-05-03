CREATE TABLE IF NOT EXISTS source (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS online_manga (
    source_id TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title TEXT NOT NULL,
    cover_url TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, external_id),
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS online_chapter (
    source_id TEXT NOT NULL,
    external_manga_id TEXT NOT NULL,
    external_chapter_id TEXT NOT NULL,
    title TEXT NOT NULL,
    chapter_order INTEGER NOT NULL DEFAULT 0,
    page_count INTEGER NOT NULL DEFAULT 0,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, external_chapter_id),
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS download_job (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    external_manga_id TEXT NOT NULL,
    manga_title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'chapters',
    total_chapters INTEGER NOT NULL DEFAULT 0,
    done_chapters INTEGER NOT NULL DEFAULT 0,
    total_pages INTEGER NOT NULL DEFAULT 0,
    done_pages INTEGER NOT NULL DEFAULT 0,
    failed_pages INTEGER NOT NULL DEFAULT 0,
    concurrency INTEGER NOT NULL DEFAULT 3,
    request_interval_ms INTEGER NOT NULL DEFAULT 800,
    output_root TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    finished_at DATETIME,
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS download_job_chapter (
    job_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    external_manga_id TEXT NOT NULL,
    external_chapter_id TEXT NOT NULL,
    title TEXT NOT NULL,
    chapter_order INTEGER NOT NULL DEFAULT 0,
    page_count INTEGER NOT NULL DEFAULT 0,
    done_pages INTEGER NOT NULL DEFAULT 0,
    failed_pages INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (job_id, external_chapter_id),
    FOREIGN KEY (job_id) REFERENCES download_job(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS download_item (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    external_chapter_id TEXT NOT NULL,
    page_index INTEGER NOT NULL,
    remote_url TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued',
    error TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (job_id, external_chapter_id, page_index),
    FOREIGN KEY (job_id) REFERENCES download_job(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_online_manga_last_seen
ON online_manga(source_id ASC, last_seen_at DESC, title ASC);

CREATE INDEX IF NOT EXISTS idx_online_chapter_manga
ON online_chapter(source_id ASC, external_manga_id ASC, chapter_order ASC, external_chapter_id ASC);

CREATE INDEX IF NOT EXISTS idx_download_job_status
ON download_job(status ASC, updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_download_item_status
ON download_item(job_id ASC, status ASC, external_chapter_id ASC, page_index ASC);

INSERT OR IGNORE INTO source (id, name, base_url, enabled)
VALUES ('18comic', '18comic', 'https://18comic.vip', 0);
