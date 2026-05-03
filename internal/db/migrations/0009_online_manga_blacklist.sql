CREATE TABLE IF NOT EXISTS online_manga_blacklist (
    source_id TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_id, external_id),
    FOREIGN KEY (source_id) REFERENCES source(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_online_manga_blacklist_source
ON online_manga_blacklist(source_id ASC, external_id ASC);
