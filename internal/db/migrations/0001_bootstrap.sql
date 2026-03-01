CREATE TABLE IF NOT EXISTS manga (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    title_sort TEXT,
    path TEXT NOT NULL UNIQUE,
    cover_page_id TEXT,
    chapter_count INTEGER NOT NULL DEFAULT 0,
    page_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_scan_at DATETIME
);

CREATE TABLE IF NOT EXISTS chapter (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    title TEXT NOT NULL,
    number REAL,
    volume TEXT,
    path TEXT NOT NULL UNIQUE,
    page_count INTEGER NOT NULL DEFAULT 0,
    file_mtime DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS page (
    id TEXT PRIMARY KEY,
    chapter_id TEXT NOT NULL,
    page_index INTEGER NOT NULL,
    path TEXT NOT NULL UNIQUE,
    mime TEXT,
    width INTEGER,
    height INTEGER,
    size_bytes INTEGER,
    file_mtime DATETIME,
    checksum TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (chapter_id) REFERENCES chapter(id) ON DELETE CASCADE,
    UNIQUE (chapter_id, page_index)
);

CREATE TABLE IF NOT EXISTS progress (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    chapter_id TEXT NOT NULL,
    page_index INTEGER NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (chapter_id) REFERENCES chapter(id) ON DELETE CASCADE,
    UNIQUE (manga_id)
);

CREATE TABLE IF NOT EXISTS thumbnail_cache (
    id TEXT PRIMARY KEY,
    source_type TEXT NOT NULL CHECK(source_type IN ('cover', 'page')),
    source_id TEXT NOT NULL,
    preset TEXT,
    transform_params TEXT,
    format TEXT NOT NULL CHECK(format IN ('webp', 'avif')),
    cache_path TEXT NOT NULL UNIQUE,
    byte_size INTEGER NOT NULL DEFAULT 0,
    source_mtime DATETIME,
    cache_key TEXT NOT NULL UNIQUE,
    last_access_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued', 'running', 'done', 'failed', 'canceled')),
    payload_json TEXT,
    progress_json TEXT,
    started_at DATETIME,
    finished_at DATETIME,
    error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_manga_title_sort ON manga(title_sort);
CREATE INDEX IF NOT EXISTS idx_manga_updated_at ON manga(updated_at);
CREATE INDEX IF NOT EXISTS idx_chapter_manga_number ON chapter(manga_id, number);
CREATE INDEX IF NOT EXISTS idx_page_chapter_index ON page(chapter_id, page_index);
CREATE INDEX IF NOT EXISTS idx_thumbnail_cache_lookup ON thumbnail_cache(source_type, source_id, preset, format);
CREATE INDEX IF NOT EXISTS idx_task_type_status_started ON task(type, status, started_at);
