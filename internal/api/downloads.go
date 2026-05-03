package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	downloadsvc "mynewmangaui/internal/download"
)

type downloadHandler struct {
	service *downloadsvc.Service
}

type createDownloadJobRequest struct {
	ChapterIDs []string `json:"chapterIds"`
	Mode       string   `json:"mode"`
}

type downloadJobsResponse struct {
	Items []any `json:"items"`
}

func newDownloadHandler(service *downloadsvc.Service) *downloadHandler {
	return &downloadHandler{service: service}
}

func (h *downloadHandler) listJobs(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeJSON(w, http.StatusOK, downloadJobsResponse{Items: []any{}})
		return
	}

	items, err := h.service.ListJobs(r.Context(), parsePositiveInt(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *downloadHandler) getJob(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "download service not initialized")
		return
	}

	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	item, err := h.service.GetJob(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *downloadHandler) createJob(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "download service not initialized")
		return
	}

	sourceID := strings.TrimSpace(chi.URLParam(r, "sourceID"))
	mangaID := strings.TrimSpace(chi.URLParam(r, "mangaID"))

	var request createDownloadJobRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	item, existing, err := h.service.CreateJob(r.Context(), downloadsvc.CreateJobInput{
		SourceID:   sourceID,
		MangaID:    mangaID,
		ChapterIDs: request.ChapterIDs,
		Mode:       request.Mode,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item.Existing = existing
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	writeJSON(w, status, item)
}

func (h *downloadHandler) pauseJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, h.service.PauseJob)
}

func (h *downloadHandler) resumeJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, h.service.ResumeJob)
}

func (h *downloadHandler) cancelJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, h.service.CancelJob)
}

func (h *downloadHandler) retryJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, h.service.RetryJob)
}

func (h *downloadHandler) redownloadJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, h.service.RedownloadJob)
}

func (h *downloadHandler) deleteJobRecord(w http.ResponseWriter, r *http.Request) {
	h.deleteJob(w, r, h.service.DeleteJobRecord)
}

func (h *downloadHandler) deleteJobAndFiles(w http.ResponseWriter, r *http.Request) {
	h.deleteJob(w, r, h.service.DeleteJobAndFiles)
}

func (h *downloadHandler) mutateJob(w http.ResponseWriter, r *http.Request, action func(context.Context, string) error) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "download service not initialized")
		return
	}

	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}
	if err := action(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	item, err := h.service.GetJob(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *downloadHandler) deleteJob(w http.ResponseWriter, r *http.Request, action func(context.Context, string) error) {
	if h.service == nil {
		writeError(w, http.StatusNotImplemented, "download service not initialized")
		return
	}

	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}
	if err := action(r.Context(), jobID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"jobId":   jobID,
	})
}
