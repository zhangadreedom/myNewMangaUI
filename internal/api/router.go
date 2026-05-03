package api

import (
	"database/sql"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mynewmangaui/internal/config"
	downloadsvc "mynewmangaui/internal/download"
	imagesvc "mynewmangaui/internal/image"
	onlinesvc "mynewmangaui/internal/online"
	scansvc "mynewmangaui/internal/scan"
)

//go:embed static/*
var staticFiles embed.FS

type Dependencies struct {
	Logger      *slog.Logger
	Config      config.Config
	DB          *sql.DB
	Scanner     *scansvc.Service
	Images      *imagesvc.Service
	Online      *onlinesvc.Service
	OnlineCache *onlinesvc.CacheService
	Downloads   *downloadsvc.Service
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	library := newLibraryHandler(deps.DB, deps.Config.Storage.Bookshelves)
	manga := newMangaHandler(deps.DB)
	tags := newTagHandler(deps.DB)
	images := newImageHandler(deps.DB, deps.Images)
	scan := newScanHandler(deps.Scanner)
	online := newOnlineHandler(deps.DB, deps.Online, deps.OnlineCache)
	downloads := newDownloadHandler(deps.Downloads)
	staticFS := mustStaticFS()
	access, err := newAccessControl(deps.Config.Server)
	if err != nil {
		panic(err)
	}

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(requestLogger(deps.Logger))
	r.Use(access.middleware)

	r.Get("/auth/login", access.loginPage)
	r.Post("/auth/login", access.loginSubmit)
	r.Post("/auth/logout", access.logout)
	r.Get("/health", healthHandler)
	r.Get("/api/bookshelves", library.getBookshelves)
	r.Get("/api/library", library.getLibrary)
	r.Get("/api/tags", tags.getTags)
	r.Post("/api/tags", tags.createTag)
	r.Put("/api/tags/reorder", tags.reorderTags)
	r.Put("/api/tags/{tagID}", tags.updateTag)
	r.Delete("/api/tags/{tagID}", tags.deleteTag)
	r.Get("/api/manga/{mangaID}", manga.getManga)
	r.Put("/api/manga/{mangaID}/tags", tags.updateMangaTags)
	r.Get("/api/manga/{mangaID}/chapters", manga.getChapters)
	r.Get("/api/chapters/{chapterID}/pages", manga.getChapterPages)
	r.Get("/api/images/covers/{mangaID}/thumb", images.getCoverThumb)
	r.Get("/api/images/chapters/{chapterID}/pages/{pageIndex}", images.getChapterPage)
	r.Get("/api/online/sources", online.listSources)
	r.Get("/api/online/settings", online.listSettings)
	r.Get("/api/online/{sourceID}/default", online.defaultFeed)
	r.Post("/api/online/{sourceID}/default/refresh", online.refreshDefaultFeed)
	r.Get("/api/online/{sourceID}/search", online.search)
	r.Put("/api/online/{sourceID}/settings", online.updateSettings)
	r.Get("/api/online/{sourceID}/manga/{mangaID}", online.getManga)
	r.Post("/api/online/{sourceID}/manga/{mangaID}/block", online.blockManga)
	r.Get("/api/online/{sourceID}/manga/{mangaID}/chapters", online.getChapters)
	r.Get("/api/online/{sourceID}/chapters/{chapterID}/pages", online.getPages)
	r.Get("/api/online/{sourceID}/image", online.getImage)
	r.Get("/api/online/downloads", downloads.listJobs)
	r.Get("/api/online/downloads/{jobID}", downloads.getJob)
	r.Post("/api/online/{sourceID}/manga/{mangaID}/download", downloads.createJob)
	r.Post("/api/online/downloads/{jobID}/pause", downloads.pauseJob)
	r.Post("/api/online/downloads/{jobID}/resume", downloads.resumeJob)
	r.Post("/api/online/downloads/{jobID}/cancel", downloads.cancelJob)
	r.Post("/api/online/downloads/{jobID}/retry", downloads.retryJob)
	r.Post("/api/online/downloads/{jobID}/redownload", downloads.redownloadJob)
	r.Delete("/api/online/downloads/{jobID}", downloads.deleteJobRecord)
	r.Delete("/api/online/downloads/{jobID}/files", downloads.deleteJobAndFiles)
	r.Get("/api/tasks/scan/status", scan.getScanStatus)
	r.Post("/api/tasks/scan", scan.triggerScan)
	r.Post("/api/tasks/scan/bookshelf/{bookshelfID}", scan.triggerBookshelfScan)
	r.Post("/api/tasks/scan/manga/{mangaID}", scan.triggerMangaScan)
	r.Post("/api/tasks/scan/tag/{tagID}", scan.triggerTagScan)
	r.Handle("/*", http.FileServer(http.FS(staticFS)))

	return r
}

func mustStaticFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			logger.Info("request complete",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
