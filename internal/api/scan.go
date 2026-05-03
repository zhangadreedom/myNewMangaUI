package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	scansvc "mynewmangaui/internal/scan"
)

type scanHandler struct {
	scanner *scansvc.Service
}

func newScanHandler(scanner *scansvc.Service) *scanHandler {
	return &scanHandler{scanner: scanner}
}

func (h *scanHandler) triggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusInternalServerError, "scan service not initialized")
		return
	}
	if h.scanner.Status().Running {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "running",
			"scan":   h.scanner.Status(),
		})
		return
	}

	go func() {
		_, _ = h.scanner.Scan(context.Background())
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "accepted",
	})
}

func (h *scanHandler) getScanStatus(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusInternalServerError, "scan service not initialized")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"scan":   h.scanner.Status(),
	})
}

func (h *scanHandler) triggerMangaScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusInternalServerError, "scan service not initialized")
		return
	}

	mangaID := chi.URLParam(r, "mangaID")
	summary, err := h.scanner.ScanManga(r.Context(), mangaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "manga scan failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"summary": summary,
	})
}

func (h *scanHandler) triggerBookshelfScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusInternalServerError, "scan service not initialized")
		return
	}

	bookshelfID := chi.URLParam(r, "bookshelfID")
	summary, err := h.scanner.ScanBookshelf(r.Context(), bookshelfID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bookshelf scan failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"summary": summary,
	})
}

func (h *scanHandler) triggerTagScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		writeError(w, http.StatusInternalServerError, "scan service not initialized")
		return
	}

	tagID := chi.URLParam(r, "tagID")
	summary, err := h.scanner.ScanTag(r.Context(), tagID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tag scan failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"summary": summary,
	})
}
