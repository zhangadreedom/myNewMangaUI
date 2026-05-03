package api

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

type tagItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Color    string `json:"color"`
	Group    string `json:"group"`
	Priority int    `json:"priority"`
	Pinned   bool   `json:"pinned"`
	Count    int    `json:"count"`
}

type tagsResponse struct {
	Items []tagItem `json:"items"`
}

type tagUpsertRequest struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	Group    string `json:"group"`
	Priority int    `json:"priority"`
	Pinned   bool   `json:"pinned"`
}

type mangaTagsUpdateRequest struct {
	TagIDs []string `json:"tagIds"`
}

type tagReorderRequest struct {
	OrderedIDs []string `json:"orderedIds"`
}

type tagHandler struct {
	db *sql.DB
}

func newTagHandler(db *sql.DB) *tagHandler {
	return &tagHandler{db: db}
}

func (h *tagHandler) getTags(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	items, err := loadTags(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tags")
		return
	}

	writeJSON(w, http.StatusOK, tagsResponse{Items: items})
}

func (h *tagHandler) createTag(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	request, err := decodeTagUpsertRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slug := slugifyTagName(request.Name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "tag name is required")
		return
	}

	baseID := "tag_" + shortHash(slug)
	nextID := baseID
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			nextID = fmt.Sprintf("%s_%d", baseID, attempt+1)
		}
		if _, err := h.db.ExecContext(r.Context(), `
			INSERT INTO tag(id, name, slug, color, group_name, priority, sort_order, is_pinned)
			VALUES(?, ?, ?, ?, ?, ?, COALESCE((SELECT MAX(sort_order) + 10 FROM tag), 10), ?)
		`, nextID, request.Name, slug, request.Color, request.Group, request.Priority, boolToInt(request.Pinned)); err == nil {
			items, loadErr := loadTags(r.Context(), h.db)
			if loadErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to reload tags")
				return
			}
			writeJSON(w, http.StatusCreated, tagsResponse{Items: items})
			return
		} else if strings.Contains(strings.ToLower(err.Error()), "unique") {
			if tagExistsWithSlug(r.Context(), h.db, slug, nextID) {
				writeError(w, http.StatusConflict, "tag name already exists")
				return
			}
			continue
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create tag")
			return
		}
	}

	writeError(w, http.StatusConflict, "failed to allocate unique tag id")
}

func (h *tagHandler) updateTag(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	tagID := chi.URLParam(r, "tagID")
	if strings.TrimSpace(tagID) == "" {
		writeError(w, http.StatusBadRequest, "tag id is required")
		return
	}

	request, err := decodeTagUpsertRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slug := slugifyTagName(request.Name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "tag name is required")
		return
	}

	result, err := h.db.ExecContext(r.Context(), `
		UPDATE tag
		SET name = ?, slug = ?, color = ?, group_name = ?, priority = ?, is_pinned = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, request.Name, slug, request.Color, request.Group, request.Priority, boolToInt(request.Pinned), tagID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "tag name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update tag")
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}

	items, err := loadTags(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload tags")
		return
	}
	writeJSON(w, http.StatusOK, tagsResponse{Items: items})
}

func (h *tagHandler) deleteTag(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	tagID := chi.URLParam(r, "tagID")
	if strings.TrimSpace(tagID) == "" {
		writeError(w, http.StatusBadRequest, "tag id is required")
		return
	}

	result, err := h.db.ExecContext(r.Context(), `DELETE FROM tag WHERE id = ?`, tagID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tag")
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}

	items, err := loadTags(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload tags")
		return
	}
	writeJSON(w, http.StatusOK, tagsResponse{Items: items})
}

func (h *tagHandler) reorderTags(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	var request tagReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid reorder payload")
		return
	}

	orderedIDs := normalizeTagIDs(request.OrderedIDs)
	if len(orderedIDs) == 0 {
		writeError(w, http.StatusBadRequest, "ordered tag ids are required")
		return
	}

	valid, err := validTagIDs(r.Context(), h.db, orderedIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate tags")
		return
	}
	if len(valid) != len(orderedIDs) {
		writeError(w, http.StatusBadRequest, "one or more tags are invalid")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start reorder")
		return
	}

	for index, tagID := range orderedIDs {
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE tag
			SET sort_order = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, (index+1)*10, tagID); err != nil {
			tx.Rollback()
			writeError(w, http.StatusInternalServerError, "failed to reorder tags")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save reorder")
		return
	}

	items, err := loadTags(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload tags")
		return
	}
	writeJSON(w, http.StatusOK, tagsResponse{Items: items})
}

func (h *tagHandler) updateMangaTags(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	mangaID := chi.URLParam(r, "mangaID")
	if strings.TrimSpace(mangaID) == "" {
		writeError(w, http.StatusBadRequest, "manga id is required")
		return
	}

	var request mangaTagsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tag payload")
		return
	}

	tagIDs := normalizeTagIDs(request.TagIDs)

	exists, err := mangaExists(r.Context(), h.db, mangaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load manga")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "manga not found")
		return
	}

	validTagIDs, err := validTagIDs(r.Context(), h.db, tagIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate tags")
		return
	}
	if len(validTagIDs) != len(tagIDs) {
		writeError(w, http.StatusBadRequest, "one or more tags are invalid")
		return
	}

	if err := replaceMangaTags(r.Context(), h.db, mangaID, tagIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update manga tags")
		return
	}

	items, err := loadMangaTags(r.Context(), h.db, mangaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload manga tags")
		return
	}

	writeJSON(w, http.StatusOK, tagsResponse{Items: items})
}

func decodeTagUpsertRequest(r *http.Request) (tagUpsertRequest, error) {
	var request tagUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return tagUpsertRequest{}, fmt.Errorf("invalid tag payload")
	}

	request.Name = strings.TrimSpace(request.Name)
	request.Group = strings.TrimSpace(request.Group)
	request.Color = normalizeTagColor(request.Color)
	if request.Name == "" {
		return tagUpsertRequest{}, fmt.Errorf("tag name is required")
	}
	if request.Color == "" {
		request.Color = "#c77757"
	}
	return request, nil
}

func loadTags(ctx context.Context, db *sql.DB) ([]tagItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			t.id, t.name, t.slug, t.color, t.group_name, t.priority, t.is_pinned,
			COUNT(DISTINCT mt.manga_id) AS manga_count
		FROM tag t
		LEFT JOIN manga_tag mt ON mt.tag_id = t.id
		GROUP BY t.id, t.name, t.slug, t.color, t.group_name, t.priority, t.is_pinned, t.sort_order
		ORDER BY t.is_pinned DESC, t.priority DESC, t.sort_order ASC, t.name ASC, t.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]tagItem, 0)
	for rows.Next() {
		var item tagItem
		var pinned int
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Color, &item.Group, &item.Priority, &pinned, &item.Count); err != nil {
			return nil, err
		}
		item.Pinned = pinned > 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func loadMangaTags(ctx context.Context, db *sql.DB, mangaID string) ([]tagItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.name, t.slug, t.color, t.group_name, t.priority, t.is_pinned
		FROM manga_tag mt
		JOIN tag t ON t.id = mt.tag_id
		WHERE mt.manga_id = ?
		ORDER BY t.is_pinned DESC, t.priority DESC, t.sort_order ASC, t.name ASC, t.id ASC
	`, mangaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]tagItem, 0)
	for rows.Next() {
		var item tagItem
		var pinned int
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Color, &item.Group, &item.Priority, &pinned); err != nil {
			return nil, err
		}
		item.Pinned = pinned > 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func mangaExists(ctx context.Context, db *sql.DB, mangaID string) (bool, error) {
	var found string
	err := db.QueryRowContext(ctx, `SELECT id FROM manga WHERE id = ?`, mangaID).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func tagExistsWithSlug(ctx context.Context, db *sql.DB, slug, excludeID string) bool {
	var found string
	err := db.QueryRowContext(ctx, `
		SELECT id
		FROM tag
		WHERE slug = ? AND id <> ?
		LIMIT 1
	`, slug, excludeID).Scan(&found)
	return err == nil
}

func validTagIDs(ctx context.Context, db *sql.DB, tagIDs []string) (map[string]struct{}, error) {
	if len(tagIDs) == 0 {
		return map[string]struct{}{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id
		FROM tag
		WHERE id IN (`+placeholders(len(tagIDs))+`)
	`, toAnySlice(tagIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	valid := make(map[string]struct{}, len(tagIDs))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		valid[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return valid, nil
}

func replaceMangaTags(ctx context.Context, db *sql.DB, mangaID string, tagIDs []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM manga_tag WHERE manga_id = ?`, mangaID); err != nil {
		tx.Rollback()
		return err
	}

	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manga_tag(manga_id, tag_id)
			VALUES(?, ?)
		`, mangaID, tagID); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func normalizeTagIDs(tagIDs []string) []string {
	seen := make(map[string]struct{}, len(tagIDs))
	items := make([]string, 0, len(tagIDs))
	for _, tagID := range tagIDs {
		tagID = strings.TrimSpace(tagID)
		if tagID == "" {
			continue
		}
		if _, ok := seen[tagID]; ok {
			continue
		}
		seen[tagID] = struct{}{}
		items = append(items, tagID)
	}
	return items
}

func slugifyTagName(name string) string {
	normalized := strings.TrimSpace(strings.ToLower(name))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = regexp.MustCompile(`[^a-z0-9\-\p{Han}]`).ReplaceAllString(normalized, "")
	normalized = regexp.MustCompile(`-+`).ReplaceAllString(normalized, "-")
	return strings.Trim(normalized, "-")
}

func normalizeTagColor(color string) string {
	color = strings.TrimSpace(color)
	if color == "" {
		return ""
	}
	if !strings.HasPrefix(color, "#") {
		color = "#" + color
	}
	if matched, _ := regexp.MatchString(`^#[0-9a-fA-F]{6}$`, color); !matched {
		return "#c77757"
	}
	return strings.ToLower(color)
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:6])
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}

func toAnySlice(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}
