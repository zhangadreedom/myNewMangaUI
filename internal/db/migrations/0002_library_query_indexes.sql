CREATE INDEX IF NOT EXISTS idx_manga_library_order
ON manga(updated_at DESC, title_sort ASC, id ASC);
