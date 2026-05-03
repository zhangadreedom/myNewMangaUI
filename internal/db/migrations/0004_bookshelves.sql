CREATE TABLE IF NOT EXISTS bookshelf (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE manga ADD COLUMN bookshelf_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_bookshelf_sort_order
ON bookshelf(sort_order ASC, name ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_manga_bookshelf_updated
ON manga(bookshelf_id ASC, updated_at DESC, title ASC, id ASC);
