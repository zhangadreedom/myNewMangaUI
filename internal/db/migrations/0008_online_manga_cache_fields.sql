ALTER TABLE online_manga ADD COLUMN source_url TEXT NOT NULL DEFAULT '';
ALTER TABLE online_manga ADD COLUMN chapter_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE online_manga ADD COLUMN page_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE online_manga ADD COLUMN cover_cached_at DATETIME;
ALTER TABLE online_manga ADD COLUMN detail_checked_at DATETIME;
ALTER TABLE online_manga ADD COLUMN cache_status TEXT NOT NULL DEFAULT 'partial';

CREATE INDEX IF NOT EXISTS idx_online_manga_cache_status
ON online_manga(source_id ASC, cache_status ASC, last_fetched_at ASC);
