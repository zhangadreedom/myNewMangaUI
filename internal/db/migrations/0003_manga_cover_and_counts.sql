ALTER TABLE manga ADD COLUMN cover_path TEXT NOT NULL DEFAULT '';
ALTER TABLE manga ADD COLUMN page_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE manga ADD COLUMN last_scan_at DATETIME;

ALTER TABLE chapter ADD COLUMN page_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_chapter_library_order
ON chapter(manga_id, chapter_number ASC, title ASC, id ASC);
