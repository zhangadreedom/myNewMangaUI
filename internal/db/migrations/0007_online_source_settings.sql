CREATE TABLE IF NOT EXISTS online_source_settings (
    source_id TEXT PRIMARY KEY,
    blacklisted_tags_json TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
