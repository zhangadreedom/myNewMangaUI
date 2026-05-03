package online

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

const (
	CacheStatusPartial = "partial"
	CacheStatusDetail  = "detail"

	defaultCacheRefreshPages        = 5
	defaultCacheDetailHydrateBudget = 30
)

type CacheService struct {
	db     *sql.DB
	online *Service
	logger *slog.Logger
}

func NewCacheService(db *sql.DB, online *Service, logger *slog.Logger) *CacheService {
	return &CacheService{
		db:     db,
		online: online,
		logger: logger,
	}
}

func (s *CacheService) UpsertMangas(ctx context.Context, sourceID string, items []Manga, status string) error {
	for _, item := range items {
		if strings.TrimSpace(item.SourceID) == "" {
			item.SourceID = sourceID
		}
		if err := s.UpsertManga(ctx, item, status); err != nil {
			return err
		}
	}
	return nil
}

func (s *CacheService) UpsertManga(ctx context.Context, item Manga, status string) error {
	if s == nil || s.db == nil {
		return nil
	}

	item.SourceID = strings.TrimSpace(item.SourceID)
	item.ID = strings.TrimSpace(item.ID)
	item.Title = strings.TrimSpace(item.Title)
	if item.SourceID == "" || item.ID == "" {
		return fmt.Errorf("source id and manga id are required")
	}
	if item.Title == "" {
		item.Title = "Album " + item.ID
	}
	if status == "" {
		status = CacheStatusPartial
	}
	if item.SourceURL == "" {
		item.SourceURL = s.sourceMangaURL(item.SourceID, item.ID)
	}
	if err := s.ensureSource(ctx, item.SourceID); err != nil {
		return err
	}

	tagsPayload, err := json.Marshal(normalizeCacheTags(item.Tags))
	if err != nil {
		return err
	}
	rawPayload, _ := json.Marshal(item)
	checkedExpr := "detail_checked_at"
	if status == CacheStatusDetail {
		checkedExpr = "CURRENT_TIMESTAMP"
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO online_manga(
			source_id, external_id, title, cover_url, source_url, author, tags_json,
			chapter_count, page_count, cache_status, detail_checked_at, raw_json,
			last_seen_at, last_fetched_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CASE WHEN ? = 'detail' THEN CURRENT_TIMESTAMP ELSE NULL END, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(source_id, external_id) DO UPDATE SET
			title = excluded.title,
			cover_url = CASE WHEN excluded.cover_url <> '' THEN excluded.cover_url ELSE online_manga.cover_url END,
			source_url = CASE WHEN excluded.source_url <> '' THEN excluded.source_url ELSE online_manga.source_url END,
			author = CASE WHEN excluded.author <> '' THEN excluded.author ELSE online_manga.author END,
			tags_json = CASE WHEN excluded.tags_json <> '[]' THEN excluded.tags_json ELSE online_manga.tags_json END,
			chapter_count = CASE WHEN excluded.chapter_count > 0 THEN excluded.chapter_count ELSE online_manga.chapter_count END,
			page_count = CASE WHEN excluded.page_count > 0 THEN excluded.page_count ELSE online_manga.page_count END,
			cache_status = CASE WHEN excluded.cache_status = 'detail' THEN 'detail' ELSE online_manga.cache_status END,
			detail_checked_at = CASE WHEN excluded.cache_status = 'detail' THEN CURRENT_TIMESTAMP ELSE `+checkedExpr+` END,
			raw_json = CASE WHEN excluded.raw_json <> '' THEN excluded.raw_json ELSE online_manga.raw_json END,
			last_seen_at = CURRENT_TIMESTAMP,
			last_fetched_at = CURRENT_TIMESTAMP
	`, item.SourceID, item.ID, item.Title, item.CoverURL, item.SourceURL, item.Author, string(tagsPayload), item.ChapterCount, item.PageCount, status, status, string(rawPayload))
	return err
}

func (s *CacheService) MarkCoverCached(ctx context.Context, sourceID string, mangaID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE online_manga
		SET cover_cached_at = CURRENT_TIMESTAMP
		WHERE source_id = ? AND external_id = ?
	`, strings.TrimSpace(sourceID), strings.TrimSpace(mangaID))
	return err
}

func (s *CacheService) UpsertChapters(ctx context.Context, sourceID string, mangaID string, chapters []Chapter) error {
	if s == nil || s.db == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	mangaID = strings.TrimSpace(mangaID)
	if sourceID == "" || mangaID == "" {
		return fmt.Errorf("source id and manga id are required")
	}
	if err := s.ensureSource(ctx, sourceID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	totalPages := 0
	for _, chapter := range chapters {
		chapter.SourceID = strings.TrimSpace(chapter.SourceID)
		if chapter.SourceID == "" {
			chapter.SourceID = sourceID
		}
		chapter.MangaID = strings.TrimSpace(chapter.MangaID)
		if chapter.MangaID == "" {
			chapter.MangaID = mangaID
		}
		chapter.ID = strings.TrimSpace(chapter.ID)
		if chapter.ID == "" {
			continue
		}
		if strings.TrimSpace(chapter.Title) == "" {
			chapter.Title = "Chapter " + chapter.ID
		}
		rawPayload, _ := json.Marshal(chapter)
		totalPages += chapter.PageCount
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO online_chapter(
				source_id, external_manga_id, external_chapter_id, title,
				chapter_order, page_count, raw_json, last_seen_at, last_fetched_at
			) VALUES(?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(source_id, external_chapter_id) DO UPDATE SET
				external_manga_id = excluded.external_manga_id,
				title = excluded.title,
				chapter_order = excluded.chapter_order,
				page_count = excluded.page_count,
				raw_json = excluded.raw_json,
				last_seen_at = CURRENT_TIMESTAMP,
				last_fetched_at = CURRENT_TIMESTAMP
		`, sourceID, mangaID, chapter.ID, chapter.Title, chapter.Order, chapter.PageCount, string(rawPayload)); err != nil {
			tx.Rollback()
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE online_manga
		SET chapter_count = ?, page_count = CASE WHEN ? > 0 THEN ? ELSE page_count END, last_fetched_at = CURRENT_TIMESTAMP
		WHERE source_id = ? AND external_id = ?
	`, len(chapters), totalPages, totalPages, sourceID, mangaID); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *CacheService) StartBackgroundRefresh(ctx context.Context, interval time.Duration) {
	if s == nil || s.online == nil || s.db == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := s.RefreshDefaultFeeds(ctx); err != nil && s.logger != nil {
					s.logger.Warn("online cache refresh failed", "error", err)
				}
				timer.Reset(interval)
			}
		}
	}()
}

func (s *CacheService) RefreshDefaultFeeds(ctx context.Context) error {
	if s == nil || s.online == nil {
		return nil
	}
	for _, source := range s.online.ListSources() {
		if !source.Enabled {
			continue
		}
		if err := s.RefreshSourceDefaultFeed(ctx, source.ID, defaultCacheRefreshPages, source.DefaultDisplay.Limit); err != nil {
			if s.logger != nil {
				s.logger.Warn("online source cache refresh failed", "sourceID", source.ID, "error", err)
			}
			continue
		}
	}
	return nil
}

func (s *CacheService) RefreshSourceDefaultFeed(ctx context.Context, sourceID string, pages int, limit int) error {
	if s == nil || s.online == nil {
		return nil
	}
	if pages <= 0 {
		pages = 1
	}
	if limit <= 0 {
		limit = 30
	}

	seen := make(map[string]struct{}, pages*limit)
	items := make([]Manga, 0, pages*limit)
	for page := 1; page <= pages; page++ {
		feed, err := s.online.DefaultFeed(ctx, sourceID, page, limit)
		if err != nil {
			return err
		}
		for _, item := range feed.Items {
			item.SourceID = sourceID
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			items = append(items, item)
			if err := s.UpsertManga(ctx, item, CacheStatusPartial); err != nil {
				return err
			}
		}
		if !feed.HasMore {
			break
		}
	}

	for index, item := range items {
		if index >= defaultCacheDetailHydrateBudget {
			break
		}
		item = s.hydrateMangaForCache(ctx, item)
		if err := s.UpsertManga(ctx, item, cacheStatusForManga(item)); err != nil {
			return err
		}
		if chapters, err := s.online.GetChapters(ctx, sourceID, item.ID); err == nil {
			_ = s.UpsertChapters(ctx, sourceID, item.ID, chapters)
		}
		if item.CoverURL != "" {
			if _, _, err := s.online.FetchImage(ctx, sourceID, item.CoverURL); err == nil {
				_ = s.MarkCoverCached(ctx, sourceID, item.ID)
			}
		}
	}
	return nil
}

func (s *CacheService) RefreshSourceDefaultFeedList(ctx context.Context, sourceID string, pages int, limit int) error {
	if s == nil || s.online == nil {
		return nil
	}
	if pages <= 0 {
		pages = 1
	}
	if limit <= 0 {
		limit = 30
	}

	seen := make(map[string]struct{}, pages*limit)
	for page := 1; page <= pages; page++ {
		feed, err := s.online.DefaultFeed(ctx, sourceID, page, limit)
		if err != nil {
			return err
		}
		for _, item := range feed.Items {
			item.SourceID = sourceID
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			if err := s.UpsertManga(ctx, item, CacheStatusPartial); err != nil {
				return err
			}
		}
		if !feed.HasMore {
			break
		}
	}
	return nil
}

func (s *CacheService) hydrateMangaForCache(ctx context.Context, item Manga) Manga {
	if s == nil || s.online == nil || strings.TrimSpace(item.ID) == "" {
		return item
	}
	if len(item.Tags) > 0 && strings.TrimSpace(item.Author) != "" {
		return item
	}
	detail, err := s.online.GetManga(ctx, item.SourceID, item.ID)
	if err != nil {
		return item
	}
	if strings.TrimSpace(detail.Title) != "" {
		item.Title = detail.Title
	}
	if strings.TrimSpace(detail.CoverURL) != "" {
		item.CoverURL = detail.CoverURL
	}
	if strings.TrimSpace(detail.Author) != "" {
		item.Author = detail.Author
	}
	if len(detail.Tags) > 0 {
		item.Tags = detail.Tags
	}
	return item
}

func cacheStatusForManga(item Manga) string {
	if len(item.Tags) > 0 || strings.TrimSpace(item.Author) != "" {
		return CacheStatusDetail
	}
	return CacheStatusPartial
}

func (s *CacheService) CachedDefaultFeed(ctx context.Context, sourceID string, page int, limit int, blacklistedTags []string, blacklistedMangaIDs []string) (DefaultFeed, error) {
	if s == nil || s.db == nil {
		return DefaultFeed{}, fmt.Errorf("online cache is not initialized")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 30
	}

	source := s.source(sourceID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			source_id, external_id, title, cover_url, source_url, author, tags_json,
			chapter_count, page_count, cache_status,
			COALESCE(last_seen_at, ''), COALESCE(last_fetched_at, ''), COALESCE(detail_checked_at, '')
		FROM online_manga
		WHERE source_id = ?
		ORDER BY last_seen_at DESC, external_id DESC
	`, strings.TrimSpace(sourceID))
	if err != nil {
		return DefaultFeed{}, err
	}
	defer rows.Close()

	start := (page - 1) * limit
	stop := start + limit
	kept := make([]Manga, 0, stop+1)
	blacklist := buildCacheBlacklist(blacklistedTags)
	blockedIDs := buildCacheBlockedMangaIDs(blacklistedMangaIDs)
	for rows.Next() {
		item, err := scanCachedManga(rows)
		if err != nil {
			return DefaultFeed{}, err
		}
		if _, blocked := blockedIDs[item.ID]; blocked {
			continue
		}
		if cacheMangaBlocked(item.Tags, blacklist) {
			continue
		}
		kept = append(kept, item)
		if len(kept) > stop {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return DefaultFeed{}, err
	}

	items := []Manga{}
	if start < len(kept) {
		end := stop
		if end > len(kept) {
			end = len(kept)
		}
		items = kept[start:end]
	}

	title := strings.TrimSpace(source.DefaultDisplay.Title)
	if title == "" {
		title = source.Name
	}
	mode := strings.TrimSpace(source.DefaultDisplay.Mode)
	if mode == "" {
		mode = "latest"
	}
	hasMore := len(kept) > stop
	if !hasMore && len(items) == limit {
		hasMore = true
	}

	return DefaultFeed{
		SourceID:    source.ID,
		Mode:        mode,
		Title:       title,
		Description: strings.TrimSpace(source.DefaultDisplay.Description),
		Page:        page,
		Limit:       limit,
		HasMore:     hasMore,
		Items:       items,
	}, nil
}

func (s *CacheService) CachedManga(ctx context.Context, sourceID string, mangaID string) (Manga, bool, error) {
	if s == nil || s.db == nil {
		return Manga{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			source_id, external_id, title, cover_url, source_url, author, tags_json,
			chapter_count, page_count, cache_status,
			COALESCE(last_seen_at, ''), COALESCE(last_fetched_at, ''), COALESCE(detail_checked_at, '')
		FROM online_manga
		WHERE source_id = ? AND external_id = ?
	`, strings.TrimSpace(sourceID), strings.TrimSpace(mangaID))
	item, err := scanCachedManga(row)
	if err == sql.ErrNoRows {
		return Manga{}, false, nil
	}
	if err != nil {
		return Manga{}, false, err
	}
	return item, true, nil
}

func (s *CacheService) CachedChapters(ctx context.Context, sourceID string, mangaID string) ([]Chapter, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, external_manga_id, external_chapter_id, title, chapter_order, page_count, COALESCE(last_seen_at, '')
		FROM online_chapter
		WHERE source_id = ? AND external_manga_id = ?
		ORDER BY chapter_order ASC, external_chapter_id ASC
	`, strings.TrimSpace(sourceID), strings.TrimSpace(mangaID))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	items := make([]Chapter, 0)
	for rows.Next() {
		var item Chapter
		if err := rows.Scan(&item.SourceID, &item.MangaID, &item.ID, &item.Title, &item.Order, &item.PageCount, &item.LastSeenAt); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return items, len(items) > 0, nil
}

func (s *CacheService) source(sourceID string) Source {
	if s != nil && s.online != nil {
		if source, ok := s.online.sources[strings.TrimSpace(sourceID)]; ok {
			return source
		}
	}
	return Source{ID: strings.TrimSpace(sourceID), Name: strings.TrimSpace(sourceID)}
}

func (s *CacheService) ensureSource(ctx context.Context, sourceID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	source := s.source(sourceID)
	if strings.TrimSpace(source.ID) == "" {
		source.ID = strings.TrimSpace(sourceID)
	}
	if strings.TrimSpace(source.Name) == "" {
		source.Name = source.ID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source(id, name, base_url, enabled, updated_at)
		VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			enabled = excluded.enabled,
			updated_at = CURRENT_TIMESTAMP
	`, source.ID, source.Name, source.BaseURL, boolToCacheInt(source.Enabled))
	return err
}

func (s *CacheService) sourceMangaURL(sourceID string, mangaID string) string {
	source := s.source(sourceID)
	base := strings.TrimSpace(source.BaseURL)
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed == nil {
		return ""
	}
	parsed.Path = joinURLPath(parsed.Path, "/album/"+strings.TrimSpace(mangaID))
	parsed.RawQuery = ""
	return parsed.String()
}

func boolToCacheInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type cacheScanTarget interface {
	Scan(dest ...any) error
}

func scanCachedManga(row cacheScanTarget) (Manga, error) {
	var item Manga
	var tagsRaw string
	if err := row.Scan(
		&item.SourceID,
		&item.ID,
		&item.Title,
		&item.CoverURL,
		&item.SourceURL,
		&item.Author,
		&tagsRaw,
		&item.ChapterCount,
		&item.PageCount,
		&item.CacheStatus,
		&item.LastSeenAt,
		&item.LastFetchedAt,
		&item.DetailCheckedAt,
	); err != nil {
		return Manga{}, err
	}
	_ = json.Unmarshal([]byte(tagsRaw), &item.Tags)
	item.Tags = normalizeCacheTags(item.Tags)
	return item, nil
}

func normalizeCacheTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, tag)
	}
	return items
}

func buildCacheBlockedMangaIDs(ids []string) map[string]struct{} {
	blocked := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		blocked[id] = struct{}{}
	}
	return blocked
}

func buildCacheBlacklist(tags []string) map[string]struct{} {
	blacklist := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		key := strings.ToLower(strings.TrimSpace(tag))
		if key != "" {
			blacklist[key] = struct{}{}
		}
	}
	return blacklist
}

func cacheMangaBlocked(tags []string, blacklist map[string]struct{}) bool {
	if len(blacklist) == 0 {
		return false
	}
	for _, tag := range tags {
		if _, ok := blacklist[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}
