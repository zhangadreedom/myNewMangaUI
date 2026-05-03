package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	onlinesvc "mynewmangaui/internal/online"
)

type onlineHandler struct {
	db      *sql.DB
	service *onlinesvc.Service
	cache   *onlinesvc.CacheService
}

func newOnlineHandler(db *sql.DB, service *onlinesvc.Service, cache *onlinesvc.CacheService) *onlineHandler {
	return &onlineHandler{db: db, service: service, cache: cache}
}

type onlineSourcesResponse struct {
	Items []onlinesvc.Source `json:"items"`
}

type onlineSearchResponse struct {
	Items []onlinesvc.Manga `json:"items"`
}

type onlineDefaultResponse struct {
	SourceID    string            `json:"sourceId"`
	Mode        string            `json:"mode"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Page        int               `json:"page"`
	Limit       int               `json:"limit"`
	HasMore     bool              `json:"hasMore"`
	Items       []onlinesvc.Manga `json:"items"`
}

type onlineChaptersResponse struct {
	Items []onlinesvc.Chapter `json:"items"`
}

type onlinePagesResponse struct {
	Items []onlinesvc.Page `json:"items"`
}

type onlineBlockResponse struct {
	SourceID string `json:"sourceId"`
	MangaID  string `json:"mangaId"`
}

type onlineSettingsResponse struct {
	Items []onlineSourceSettings `json:"items"`
}

type onlineSourceSettings struct {
	SourceID        string   `json:"sourceId"`
	BlacklistedTags []string `json:"blacklistedTags"`
}

type onlineSettingsUpdateRequest struct {
	BlacklistedTags []string `json:"blacklistedTags"`
}

func (h *onlineHandler) listSources(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, http.StatusOK, onlineSourcesResponse{Items: []onlinesvc.Source{}})
		return
	}

	writeJSON(w, http.StatusOK, onlineSourcesResponse{
		Items: h.service.ListSources(),
	})
}

func (h *onlineHandler) listSettings(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	sources := h.listAvailableSources()
	items := make([]onlineSourceSettings, 0, len(sources))
	for _, source := range sources {
		settings, err := loadOnlineSourceSettings(r.Context(), h.db, source.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load online settings")
			return
		}
		items = append(items, settings)
	}

	writeJSON(w, http.StatusOK, onlineSettingsResponse{Items: items})
}

func (h *onlineHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	sourceID := strings.TrimSpace(chi.URLParam(r, "sourceID"))
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source id is required")
		return
	}
	if !h.hasSource(sourceID) {
		writeError(w, http.StatusNotFound, "online source not found")
		return
	}

	var request onlineSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid online settings payload")
		return
	}

	settings := onlineSourceSettings{
		SourceID:        sourceID,
		BlacklistedTags: normalizeOnlineBlacklistTags(request.BlacklistedTags),
	}
	if err := saveOnlineSourceSettings(r.Context(), h.db, settings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save online settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (h *onlineHandler) search(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "online service not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	queryText := strings.TrimSpace(r.URL.Query().Get("q"))
	if queryText == "" {
		writeJSON(w, http.StatusOK, onlineSearchResponse{Items: []onlinesvc.Manga{}})
		return
	}

	page := parseOnlinePositiveInt(r.URL.Query().Get("page"), 1)
	limit := parseOnlinePositiveInt(r.URL.Query().Get("limit"), 30)
	items, err := h.service.Search(r.Context(), sourceID, onlinesvc.SearchOptions{
		Query: queryText,
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if h.cache != nil {
		_ = h.cache.UpsertMangas(r.Context(), sourceID, items, onlinesvc.CacheStatusPartial)
	}
	items = h.filterByOnlineBlacklist(r.Context(), sourceID, items)

	writeJSON(w, http.StatusOK, onlineSearchResponse{Items: items})
}

func (h *onlineHandler) defaultFeed(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeError(w, http.StatusNotImplemented, "online cache not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	page := parseOnlinePositiveInt(r.URL.Query().Get("page"), 1)
	limit := parseOnlinePositiveInt(r.URL.Query().Get("limit"), 30)

	blacklistedTags, err := h.blacklistedTags(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load online settings")
		return
	}
	blacklistedMangaIDs, err := h.blacklistedMangaIDs(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load online manga blacklist")
		return
	}
	feed, err := h.cache.CachedDefaultFeed(r.Context(), sourceID, page, limit, blacklistedTags, blacklistedMangaIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, onlineDefaultResponse{
		SourceID:    feed.SourceID,
		Mode:        feed.Mode,
		Title:       feed.Title,
		Description: feed.Description,
		Page:        feed.Page,
		Limit:       feed.Limit,
		HasMore:     feed.HasMore,
		Items:       feed.Items,
	})
}

func (h *onlineHandler) refreshDefaultFeed(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeError(w, http.StatusNotImplemented, "online cache not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	page := 1
	limit := parseOnlinePositiveInt(r.URL.Query().Get("limit"), 30)
	pages := parseOnlinePositiveInt(r.URL.Query().Get("pages"), 1)

	if err := h.cache.RefreshSourceDefaultFeedList(r.Context(), sourceID, pages, limit); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_ = h.cache.RefreshSourceDefaultFeed(ctx, sourceID, 1, limit)
	}()

	blacklistedTags, err := h.blacklistedTags(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load online settings")
		return
	}
	blacklistedMangaIDs, err := h.blacklistedMangaIDs(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load online manga blacklist")
		return
	}
	feed, err := h.cache.CachedDefaultFeed(r.Context(), sourceID, page, limit, blacklistedTags, blacklistedMangaIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, onlineDefaultResponse{
		SourceID:    feed.SourceID,
		Mode:        feed.Mode,
		Title:       feed.Title,
		Description: feed.Description,
		Page:        feed.Page,
		Limit:       feed.Limit,
		HasMore:     feed.HasMore,
		Items:       feed.Items,
	})
}

func (h *onlineHandler) blockManga(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	sourceID := strings.TrimSpace(chi.URLParam(r, "sourceID"))
	mangaID := strings.TrimSpace(chi.URLParam(r, "mangaID"))
	if sourceID == "" || mangaID == "" {
		writeError(w, http.StatusBadRequest, "source id and manga id are required")
		return
	}
	if !h.hasSource(sourceID) {
		writeError(w, http.StatusNotFound, "online source not found")
		return
	}

	if err := saveOnlineMangaBlacklistRule(r.Context(), h.db, sourceID, mangaID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save online manga blacklist")
		return
	}

	writeJSON(w, http.StatusOK, onlineBlockResponse{SourceID: sourceID, MangaID: mangaID})
}

func (h *onlineHandler) getManga(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "online service not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	mangaID := chi.URLParam(r, "mangaID")
	var cached onlinesvc.Manga
	var hasCache bool
	if h.cache != nil {
		if item, found, err := h.cache.CachedManga(r.Context(), sourceID, mangaID); err == nil {
			cached = item
			hasCache = found
		}
	}

	item, err := h.service.GetManga(r.Context(), sourceID, mangaID)
	if err != nil {
		if hasCache {
			writeJSON(w, http.StatusOK, cached)
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if hasCache {
		item.ChapterCount = cached.ChapterCount
		item.PageCount = cached.PageCount
		if strings.TrimSpace(item.SourceURL) == "" {
			item.SourceURL = cached.SourceURL
		}
	}
	if h.cache != nil {
		_ = h.cache.UpsertManga(r.Context(), item, onlinesvc.CacheStatusDetail)
	}

	writeJSON(w, http.StatusOK, item)
}

func (h *onlineHandler) getChapters(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "online service not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	mangaID := chi.URLParam(r, "mangaID")
	var cached []onlinesvc.Chapter
	var hasCache bool
	if h.cache != nil {
		if items, found, err := h.cache.CachedChapters(r.Context(), sourceID, mangaID); err == nil {
			cached = items
			hasCache = found
		}
	}

	items, err := h.service.GetChapters(r.Context(), sourceID, mangaID)
	if err != nil {
		if hasCache {
			writeJSON(w, http.StatusOK, onlineChaptersResponse{Items: cached})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if h.cache != nil {
		_ = h.cache.UpsertChapters(r.Context(), sourceID, mangaID, items)
	}

	writeJSON(w, http.StatusOK, onlineChaptersResponse{Items: items})
}

func (h *onlineHandler) getPages(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "online service not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	chapterID := chi.URLParam(r, "chapterID")
	items, err := h.service.GetPages(r.Context(), sourceID, chapterID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	for index := range items {
		items[index].ImageURL = buildOnlineImageURL(sourceID, items[index].RemoteURL)
	}

	writeJSON(w, http.StatusOK, onlinePagesResponse{Items: items})
}

func (h *onlineHandler) getImage(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "online service not initialized")
		return
	}

	sourceID := chi.URLParam(r, "sourceID")
	target := decodeOnlineImageTarget(r.URL.Query().Get("target"))
	if target == "" {
		writeError(w, http.StatusBadRequest, "missing image target")
		return
	}

	payload, mime, err := h.service.FetchImage(r.Context(), sourceID, target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(payload)
}

func parseOnlinePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func buildOnlineImageURL(sourceID string, remoteURL string) string {
	return "/api/online/" + sourceID + "/image?target=" + base64.RawURLEncoding.EncodeToString([]byte(remoteURL))
}

func decodeOnlineImageTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return string(payload)
}

func (h *onlineHandler) listAvailableSources() []onlinesvc.Source {
	if h.service == nil {
		return []onlinesvc.Source{}
	}
	return h.service.ListSources()
}

func (h *onlineHandler) hasSource(sourceID string) bool {
	for _, source := range h.listAvailableSources() {
		if source.ID == sourceID {
			return true
		}
	}
	return false
}

func (h *onlineHandler) filterByOnlineBlacklist(ctx context.Context, sourceID string, items []onlinesvc.Manga) []onlinesvc.Manga {
	if h.db == nil || len(items) == 0 {
		return items
	}

	settings, err := loadOnlineSourceSettings(ctx, h.db, sourceID)
	if err != nil {
		return items
	}
	blacklist := buildOnlineTagBlacklist(settings.BlacklistedTags)
	blockedMangaIDs, err := loadOnlineBlockedMangaIDs(ctx, h.db, sourceID)
	if err != nil {
		return items
	}
	blocked := buildOnlineMangaIDBlacklist(blockedMangaIDs)
	if len(blacklist) == 0 && len(blocked) == 0 {
		return items
	}

	filtered := make([]onlinesvc.Manga, 0, len(items))
	for _, item := range items {
		if _, ok := blocked[strings.TrimSpace(item.ID)]; ok {
			continue
		}
		tags := item.Tags
		if h.cache != nil && strings.TrimSpace(item.ID) != "" {
			if cached, found, err := h.cache.CachedManga(ctx, sourceID, item.ID); err == nil && found {
				if len(tags) == 0 {
					tags = cached.Tags
					item.Tags = cached.Tags
				}
				if strings.TrimSpace(item.Author) == "" {
					item.Author = cached.Author
				}
				if strings.TrimSpace(item.CoverURL) == "" {
					item.CoverURL = cached.CoverURL
				}
			}
		}
		if onlineMangaHasBlacklistedTag(tags, blacklist) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func (h *onlineHandler) blacklistedMangaIDs(ctx context.Context, sourceID string) ([]string, error) {
	if h.db == nil {
		return nil, nil
	}
	return loadOnlineBlockedMangaIDs(ctx, h.db, sourceID)
}

func (h *onlineHandler) blacklistedTags(ctx context.Context, sourceID string) ([]string, error) {
	if h.db == nil {
		return nil, nil
	}
	settings, err := loadOnlineSourceSettings(ctx, h.db, sourceID)
	if err != nil {
		return nil, err
	}
	return settings.BlacklistedTags, nil
}

func saveOnlineMangaBlacklistRule(ctx context.Context, db *sql.DB, sourceID string, mangaID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO online_manga_blacklist(source_id, external_id)
		VALUES(?, ?)
	`, strings.TrimSpace(sourceID), strings.TrimSpace(mangaID))
	return err
}

func loadOnlineBlockedMangaIDs(ctx context.Context, db *sql.DB, sourceID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT external_id
		FROM online_manga_blacklist
		WHERE source_id = ?
		ORDER BY created_at DESC, external_id ASC
	`, strings.TrimSpace(sourceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func loadOnlineSourceSettings(ctx context.Context, db *sql.DB, sourceID string) (onlineSourceSettings, error) {
	settings := onlineSourceSettings{SourceID: strings.TrimSpace(sourceID), BlacklistedTags: []string{}}
	if settings.SourceID == "" {
		return settings, nil
	}

	var raw string
	err := db.QueryRowContext(ctx, `
		SELECT blacklisted_tags_json
		FROM online_source_settings
		WHERE source_id = ?
	`, settings.SourceID).Scan(&raw)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}

	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return settings, nil
	}
	settings.BlacklistedTags = normalizeOnlineBlacklistTags(tags)
	return settings, nil
}

func saveOnlineSourceSettings(ctx context.Context, db *sql.DB, settings onlineSourceSettings) error {
	settings.SourceID = strings.TrimSpace(settings.SourceID)
	settings.BlacklistedTags = normalizeOnlineBlacklistTags(settings.BlacklistedTags)

	payload, err := json.Marshal(settings.BlacklistedTags)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO online_source_settings(source_id, blacklisted_tags_json, updated_at)
		VALUES(?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_id) DO UPDATE SET
			blacklisted_tags_json = excluded.blacklisted_tags_json,
			updated_at = CURRENT_TIMESTAMP
	`, settings.SourceID, string(payload))
	return err
}

func filterOnlineMangaByBlacklist(items []onlinesvc.Manga, blacklistedTags []string) []onlinesvc.Manga {
	blacklist := buildOnlineTagBlacklist(blacklistedTags)
	if len(blacklist) == 0 {
		return items
	}

	filtered := make([]onlinesvc.Manga, 0, len(items))
	for _, item := range items {
		if !onlineMangaHasBlacklistedTag(item.Tags, blacklist) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func buildOnlineMangaIDBlacklist(ids []string) map[string]struct{} {
	blacklist := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		blacklist[id] = struct{}{}
	}
	return blacklist
}

func buildOnlineTagBlacklist(blacklistedTags []string) map[string]struct{} {
	blacklist := make(map[string]struct{}, len(blacklistedTags))
	for _, tag := range blacklistedTags {
		tag = normalizeOnlineTagKey(tag)
		if tag == "" {
			continue
		}
		blacklist[tag] = struct{}{}
	}
	return blacklist
}

func onlineMangaHasBlacklistedTag(tags []string, blacklist map[string]struct{}) bool {
	for _, tag := range tags {
		if _, ok := blacklist[normalizeOnlineTagKey(tag)]; ok {
			return true
		}
	}
	return false
}

func normalizeOnlineBlacklistTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := normalizeOnlineTagKey(tag)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, tag)
	}
	return items
}

func normalizeOnlineTagKey(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
