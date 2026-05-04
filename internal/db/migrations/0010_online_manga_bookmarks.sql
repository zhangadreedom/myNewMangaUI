CREATE TABLE IF NOT EXISTS online_manga_bookmark (
    source_id TEXT NOT NULL,
    external_id TEXT NOT NULL,
    favorite_at DATETIME,
    followed_at DATETIME,
    has_update INTEGER NOT NULL DEFAULT 0,
    last_known_chapter_count INTEGER NOT NULL DEFAULT 0,
    latest_chapter_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_id, external_id),
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_online_manga_bookmark_favorite
ON online_manga_bookmark(source_id ASC, favorite_at DESC);

CREATE INDEX IF NOT EXISTS idx_online_manga_bookmark_follow
ON online_manga_bookmark(source_id ASC, has_update DESC, followed_at DESC);
