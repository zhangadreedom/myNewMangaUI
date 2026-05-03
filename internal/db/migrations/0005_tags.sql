CREATE TABLE IF NOT EXISTS tag (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL DEFAULT '#c77757',
    group_name TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_pinned INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS manga_tag (
    manga_id TEXT NOT NULL,
    tag_id TEXT NOT NULL,
    assigned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (manga_id, tag_id),
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tag(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tag_priority
ON tag(is_pinned DESC, priority DESC, sort_order ASC, name ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_manga_tag_tag
ON manga_tag(tag_id ASC, manga_id ASC);

INSERT OR IGNORE INTO tag (id, name, slug, color, group_name, priority, sort_order, is_pinned) VALUES
    ('tag_status_ongoing', '连载中', 'status-ongoing', '#b85b39', '状态', 120, 10, 1),
    ('tag_status_completed', '已完结', 'status-completed', '#8f5b49', '状态', 110, 20, 1),
    ('tag_status_short', '短篇', 'status-short', '#b47458', '状态', 100, 30, 1),
    ('tag_source_jp', '日漫', 'source-jp', '#cb7a57', '来源', 90, 10, 0),
    ('tag_source_kr', '韩漫', 'source-kr', '#c68b56', '来源', 89, 20, 0),
    ('tag_source_cn', '国漫', 'source-cn', '#bc6d47', '来源', 88, 30, 0),
    ('tag_genre_romance', '恋爱', 'genre-romance', '#d27b74', '类型', 80, 10, 0),
    ('tag_genre_fantasy', '奇幻', 'genre-fantasy', '#9b7bb5', '类型', 79, 20, 0),
    ('tag_genre_school', '校园', 'genre-school', '#7aa57f', '类型', 78, 30, 0),
    ('tag_genre_comedy', '搞笑', 'genre-comedy', '#d69a54', '类型', 77, 40, 0),
    ('tag_feature_purelove', '纯爱', 'feature-purelove', '#ce8a85', '特征', 70, 10, 0),
    ('tag_feature_harem', '后宫', 'feature-harem', '#c76657', '特征', 69, 20, 0),
    ('tag_feature_r18', 'R18', 'feature-r18', '#9f3c34', '特征', 68, 30, 1);
