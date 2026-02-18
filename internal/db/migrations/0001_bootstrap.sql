CREATE TABLE IF NOT EXISTS manga (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chapter (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    title TEXT NOT NULL,
    chapter_number REAL,
    path TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS page (
    id TEXT PRIMARY KEY,
    chapter_id TEXT NOT NULL,
    page_index INTEGER NOT NULL,
    path TEXT NOT NULL UNIQUE,
    width INTEGER,
    height INTEGER,
    mime TEXT,
    size_bytes INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (chapter_id) REFERENCES chapter(id) ON DELETE CASCADE,
    UNIQUE (chapter_id, page_index)
);

CREATE INDEX IF NOT EXISTS idx_manga_title ON manga(title);
CREATE INDEX IF NOT EXISTS idx_chapter_manga_id ON chapter(manga_id);
CREATE INDEX IF NOT EXISTS idx_page_chapter_idx ON page(chapter_id, page_index);
