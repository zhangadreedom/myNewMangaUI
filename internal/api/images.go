package api

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

	imagesvc "mynewmangaui/internal/image"
	"mynewmangaui/internal/media"
)

type imageHandler struct {
	db     *sql.DB
	images *imagesvc.Service
}

func newImageHandler(db *sql.DB, images *imagesvc.Service) *imageHandler {
	return &imageHandler{db: db, images: images}
}

func (h *imageHandler) getCoverThumb(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeError(w, http.StatusInternalServerError, "image service not initialized")
		return
	}

	mangaID := chi.URLParam(r, "mangaID")
	cacheFile, err := h.images.EnsureMangaCoverThumb(r.Context(), mangaID)
	if err != nil {
		writeError(w, http.StatusNotFound, "cover thumbnail not available")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, cacheFile)
}

func (h *imageHandler) getChapterPage(w http.ResponseWriter, r *http.Request) {
	chapterID := chi.URLParam(r, "chapterID")
	pageIndex, err := strconv.Atoi(chi.URLParam(r, "pageIndex"))
	if err != nil || pageIndex < 0 {
		writeError(w, http.StatusBadRequest, "invalid page index")
		return
	}

	var pathRef string
	var mime string
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT path, mime
		FROM page
		WHERE chapter_id = ? AND page_index = ?
	`, chapterID, pageIndex).Scan(&pathRef, &mime); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load page")
		return
	}

	ref, err := media.ParseRef(pathRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid page source")
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if ref.Kind == "file" {
		http.ServeFile(w, r, ref.Path)
		return
	}

	rc, modifiedAt, err := media.Open(pathRef)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "page source missing")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to open page source")
		return
	}
	defer rc.Close()

	if !modifiedAt.IsZero() {
		w.Header().Set("Last-Modified", modifiedAt.UTC().Format(http.TimeFormat))
	}
	_, _ = io.Copy(w, rc)
}
