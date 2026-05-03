package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type mangaHandler struct {
	db *sql.DB
}

type mangaDetailResponse struct {
	ID            string    `json:"id"`
	BookshelfID   string    `json:"bookshelfId"`
	BookshelfName string    `json:"bookshelfName"`
	Title         string    `json:"title"`
	ChapterCount  int       `json:"chapterCount"`
	PageCount     int       `json:"pageCount"`
	UpdatedAt     string    `json:"updatedAt"`
	CoverThumbURL string    `json:"coverThumbUrl"`
	Tags          []tagItem `json:"tags"`
}

type chapterItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Number    *float64 `json:"number,omitempty"`
	PageCount int      `json:"pageCount"`
	UpdatedAt string   `json:"updatedAt"`
}

type chaptersResponse struct {
	Items   []chapterItem `json:"items"`
	Total   int           `json:"total"`
	HasMore bool          `json:"hasMore"`
}

type chapterPageItem struct {
	ID        string `json:"id"`
	Index     int    `json:"index"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"sizeBytes"`
	ImageURL  string `json:"imageUrl"`
}

type chapterPagesResponse struct {
	ChapterID string            `json:"chapterId"`
	Pages     []chapterPageItem `json:"pages"`
}

func newMangaHandler(db *sql.DB) *mangaHandler {
	return &mangaHandler{db: db}
}

func (h *mangaHandler) getManga(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "mangaID")
	response := mangaDetailResponse{
		CoverThumbURL: "/api/images/covers/" + id + "/thumb",
	}

	err := h.db.QueryRowContext(r.Context(), `
		SELECT
			m.id,
			b.id,
			b.name,
			m.title,
			COUNT(c.id) AS chapter_count,
			m.page_count,
			m.updated_at
		FROM manga m
		LEFT JOIN bookshelf b ON b.id = m.bookshelf_id
		LEFT JOIN chapter c ON c.manga_id = m.id
		WHERE m.id = ?
		GROUP BY m.id, b.id, b.name, m.title, m.page_count, m.updated_at
	`, id).Scan(
		&response.ID,
		&response.BookshelfID,
		&response.BookshelfName,
		&response.Title,
		&response.ChapterCount,
		&response.PageCount,
		&response.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "manga not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load manga")
		return
	}

	tags, err := loadMangaTags(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load manga tags")
		return
	}
	response.Tags = tags

	writeJSON(w, http.StatusOK, response)
}

func (h *mangaHandler) getChapters(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "mangaID")
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, title, chapter_number, page_count, updated_at
		FROM chapter
		WHERE manga_id = ?
		ORDER BY chapter_number ASC, title ASC, id ASC
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chapters")
		return
	}
	defer rows.Close()

	items := make([]chapterItem, 0)
	for rows.Next() {
		var item chapterItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Number, &item.PageCount, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read chapter row")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate chapter rows")
		return
	}

	writeJSON(w, http.StatusOK, chaptersResponse{
		Items:   items,
		Total:   len(items),
		HasMore: false,
	})
}

func (h *mangaHandler) getChapterPages(w http.ResponseWriter, r *http.Request) {
	chapterID := chi.URLParam(r, "chapterID")
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, page_index, width, height, mime, size_bytes
		FROM page
		WHERE chapter_id = ?
		ORDER BY page_index ASC
	`, chapterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chapter pages")
		return
	}
	defer rows.Close()

	items := make([]chapterPageItem, 0)
	for rows.Next() {
		var item chapterPageItem
		if err := rows.Scan(&item.ID, &item.Index, &item.Width, &item.Height, &item.Mime, &item.SizeBytes); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read page row")
			return
		}
		item.ImageURL = "/api/images/chapters/" + chapterID + "/pages/" + strconv.Itoa(item.Index)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate page rows")
		return
	}

	writeJSON(w, http.StatusOK, chapterPagesResponse{
		ChapterID: chapterID,
		Pages:     items,
	})
}
