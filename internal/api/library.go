package api

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"mynewmangaui/internal/config"
)

const (
	defaultLibraryPage  = 1
	defaultLibraryLimit = 60
	maxLibraryLimit     = 200
)

type libraryHandler struct {
	db          *sql.DB
	bookshelves []config.BookshelfConfig
}

type libraryMangaItem struct {
	ID            string `json:"id"`
	BookshelfID   string `json:"bookshelfId"`
	Title         string `json:"title"`
	ChapterCount  int    `json:"chapterCount"`
	PageCount     int    `json:"pageCount"`
	UpdatedAt     string `json:"updatedAt"`
	CoverThumbURL string `json:"coverThumbUrl"`
}

type libraryResponse struct {
	Items       []libraryMangaItem `json:"items"`
	BookshelfID string             `json:"bookshelfId,omitempty"`
	TagIDs      []string           `json:"tagIds,omitempty"`
	Page        int                `json:"page"`
	Limit       int                `json:"limit"`
	Total       int                `json:"total"`
	HasMore     bool               `json:"hasMore"`
}

type bookshelfItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	RootPath   string `json:"rootPath"`
	MangaCount int    `json:"mangaCount"`
	PageCount  int    `json:"pageCount"`
	UpdatedAt  string `json:"updatedAt"`
}

type bookshelvesResponse struct {
	Items []bookshelfItem `json:"items"`
}

func newLibraryHandler(db *sql.DB, bookshelves []config.BookshelfConfig) *libraryHandler {
	return &libraryHandler{db: db, bookshelves: bookshelves}
}

func (h *libraryHandler) getLibrary(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	page := parsePositiveInt(r.URL.Query().Get("page"), defaultLibraryPage)
	limit := parsePositiveInt(r.URL.Query().Get("limit"), defaultLibraryLimit)
	if limit > maxLibraryLimit {
		limit = maxLibraryLimit
	}
	offset := (page - 1) * limit
	bookshelfID := strings.TrimSpace(r.URL.Query().Get("bookshelfId"))
	tagIDs := normalizeTagIDs(splitQueryValues(r.URL.Query()["tagIds"]))

	countQuery, countArgs := buildLibraryCountQuery(bookshelfID, tagIDs)

	var total int
	if err := h.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count library")
		return
	}

	listQuery, listArgs := buildLibraryListQuery(bookshelfID, tagIDs, limit, offset)

	rows, err := h.db.QueryContext(r.Context(), listQuery, listArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query library")
		return
	}
	defer rows.Close()

	items := make([]libraryMangaItem, 0, limit)
	for rows.Next() {
		var item libraryMangaItem
		if err := rows.Scan(&item.ID, &item.BookshelfID, &item.Title, &item.ChapterCount, &item.PageCount, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read library row")
			return
		}
		item.CoverThumbURL = "/api/images/covers/" + item.ID + "/thumb"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate library rows")
		return
	}

	response := libraryResponse{
		Items:       items,
		BookshelfID: bookshelfID,
		TagIDs:      tagIDs,
		Page:        page,
		Limit:       limit,
		Total:       total,
		HasMore:     offset+len(items) < total,
	}
	writeJSON(w, http.StatusOK, response)
}

func buildLibraryCountQuery(bookshelfID string, tagIDs []string) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
		SELECT COUNT(*)
		FROM manga m
	`)

	clauses, args := buildLibraryFilters(bookshelfID, tagIDs)
	builder.WriteString(" WHERE ")
	builder.WriteString(strings.Join(clauses, " AND "))
	return builder.String(), args
}

func buildLibraryListQuery(bookshelfID string, tagIDs []string, limit, offset int) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
		SELECT
			m.id,
			m.bookshelf_id,
			m.title,
			COUNT(c.id) AS chapter_count,
			m.page_count,
			m.updated_at
		FROM manga m
		LEFT JOIN chapter c ON c.manga_id = m.id
	`)

	clauses, args := buildLibraryFilters(bookshelfID, tagIDs)
	builder.WriteString(" WHERE ")
	builder.WriteString(strings.Join(clauses, " AND "))
	builder.WriteString(`
		GROUP BY m.id, m.bookshelf_id, m.title, m.page_count, m.updated_at
		ORDER BY m.updated_at DESC, m.title ASC
		LIMIT ? OFFSET ?
	`)
	args = append(args, limit, offset)
	return builder.String(), args
}

func buildLibraryFilters(bookshelfID string, tagIDs []string) ([]string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, len(tagIDs)+3)

	if bookshelfID != "" {
		clauses = append(clauses, "m.bookshelf_id = ?")
		args = append(args, bookshelfID)
	}

	if len(tagIDs) > 0 {
		clauses = append(clauses, fmt.Sprintf(`
			m.id IN (
				SELECT mt.manga_id
				FROM manga_tag mt
				WHERE mt.tag_id IN (%s)
				GROUP BY mt.manga_id
				HAVING COUNT(DISTINCT mt.tag_id) = ?
			)
		`, placeholders(len(tagIDs))))
		args = append(args, toAnySlice(tagIDs)...)
		args = append(args, len(tagIDs))
	}

	if len(clauses) == 0 {
		clauses = append(clauses, "1 = 1")
	}

	return clauses, args
}

func splitQueryValues(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			items = append(items, part)
		}
	}
	return items
}

func (h *libraryHandler) getBookshelves(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database not initialized")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT
			b.id,
			b.name,
			b.root_path,
			COUNT(m.id) AS manga_count,
			COALESCE(SUM(m.page_count), 0) AS page_count,
			COALESCE(MAX(m.updated_at), b.updated_at) AS updated_at
		FROM bookshelf b
		LEFT JOIN manga m ON m.bookshelf_id = b.id
		GROUP BY b.id, b.name, b.root_path, b.sort_order, b.updated_at
		ORDER BY b.sort_order ASC, b.name ASC, b.id ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query bookshelves")
		return
	}
	defer rows.Close()

	items := make([]bookshelfItem, 0)
	for rows.Next() {
		var item bookshelfItem
		if err := rows.Scan(&item.ID, &item.Name, &item.RootPath, &item.MangaCount, &item.PageCount, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read bookshelf row")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate bookshelves")
		return
	}

	byPath := make(map[string]bookshelfItem, len(items))
	for _, item := range items {
		byPath[normalizeBookshelfPath(item.RootPath)] = item
	}

	merged := make([]bookshelfItem, 0, len(h.bookshelves)+len(items))
	for _, shelf := range h.bookshelves {
		key := normalizeBookshelfPath(shelf.Path)
		if item, ok := byPath[key]; ok {
			if strings.TrimSpace(shelf.Name) != "" {
				item.Name = shelf.Name
			}
			merged = append(merged, item)
			delete(byPath, key)
			continue
		}

		merged = append(merged, bookshelfItem{
			ID:         bookshelfConfigID(shelf.Path),
			Name:       shelf.Name,
			RootPath:   shelf.Path,
			MangaCount: 0,
			PageCount:  0,
		})
	}

	for _, item := range byPath {
		merged = append(merged, item)
	}

	writeJSON(w, http.StatusOK, bookshelvesResponse{Items: merged})
}

func normalizeBookshelfPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(filepath.Clean(path), "\\", "/"))
}

func bookshelfConfigID(path string) string {
	normalized := normalizeBookshelfPath(path)
	sum := sha1.Sum([]byte(normalized))
	return "bs_" + hex.EncodeToString(sum[:8])
}

func parsePositiveInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
