package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultLibraryPage  = 1
	defaultLibraryLimit = 60
	maxLibraryLimit     = 200
)

type libraryHandler struct {
	db *sql.DB
}

type libraryMangaItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ChapterCount int    `json:"chapterCount"`
	UpdatedAt    string `json:"updatedAt"`
}

type libraryResponse struct {
	Items   []libraryMangaItem `json:"items"`
	Page    int                `json:"page"`
	Limit   int                `json:"limit"`
	Total   int                `json:"total"`
	HasMore bool               `json:"hasMore"`
}

func newLibraryHandler(db *sql.DB) *libraryHandler {
	return &libraryHandler{db: db}
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

	const countQuery = `SELECT COUNT(*) FROM manga`
	var total int
	if err := h.db.QueryRowContext(r.Context(), countQuery).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count library")
		return
	}

	const listQuery = `
		SELECT id, title, chapter_count, updated_at
		FROM manga
		ORDER BY updated_at DESC, title_sort ASC
		LIMIT ? OFFSET ?
	`

	rows, err := h.db.QueryContext(r.Context(), listQuery, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query library")
		return
	}
	defer rows.Close()

	items := make([]libraryMangaItem, 0, limit)
	for rows.Next() {
		var item libraryMangaItem
		if err := rows.Scan(&item.ID, &item.Title, &item.ChapterCount, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read library row")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate library rows")
		return
	}

	response := libraryResponse{
		Items:   items,
		Page:    page,
		Limit:   limit,
		Total:   total,
		HasMore: offset+len(items) < total,
	}
	writeJSON(w, http.StatusOK, response)
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
