package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"mynewmangaui/internal/api"
	"mynewmangaui/internal/config"
	"mynewmangaui/internal/db"
	downloadsvc "mynewmangaui/internal/download"
	imagesvc "mynewmangaui/internal/image"
	onlinesvc "mynewmangaui/internal/online"
	scansvc "mynewmangaui/internal/scan"
)

func main() {
	cfgPath := flag.String("config", "config.json", "Path to JSON config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err := config.EnsurePaths(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize filesystem paths: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	logger.Info("starting server", "addr", cfg.Server.Address)

	rootCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	database, err := db.OpenAndMigrate(rootCtx, cfg.Database.Path, logger)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	bookshelves := make([]scansvc.Bookshelf, 0, len(cfg.Storage.Bookshelves))
	for _, shelf := range cfg.Storage.Bookshelves {
		bookshelves = append(bookshelves, scansvc.Bookshelf{
			Name: shelf.Name,
			Path: shelf.Path,
		})
	}
	if cfg.Online.Enabled && cfg.Online.DownloadsPath != "" {
		addedOnlineShelf := false
		for _, source := range cfg.Online.Sources {
			if !source.Enabled || strings.TrimSpace(source.ID) == "" {
				continue
			}

			shelfPath := filepath.Join(cfg.Online.DownloadsPath, source.ID)
			if abs, err := filepath.Abs(shelfPath); err == nil {
				shelfPath = abs
			}
			if hasBookshelfPath(bookshelves, shelfPath) {
				continue
			}

			shelfName := "\u5728\u7ebf\u6f2b\u753b"
			if name := strings.TrimSpace(source.Name); name != "" {
				shelfName += " \u00b7 " + name
			}

			bookshelves = append(bookshelves, scansvc.Bookshelf{
				Name: shelfName,
				Path: shelfPath,
			})
			addedOnlineShelf = true
		}

		downloadsPath := cfg.Online.DownloadsPath
		if abs, err := filepath.Abs(downloadsPath); err == nil {
			downloadsPath = abs
		}
		if !addedOnlineShelf && !hasBookshelfPath(bookshelves, downloadsPath) {
			bookshelves = append(bookshelves, scansvc.Bookshelf{
				Name: "\u5728\u7ebf\u6f2b\u753b",
				Path: downloadsPath,
			})
		}
	}

	scanner := scansvc.NewService(database, bookshelves, logger)
	images := imagesvc.NewService(database, cfg.Storage.CachePath, logger)
	online, err := onlinesvc.NewDefaultService(cfg.Online)
	if err != nil {
		logger.Error("online service initialization failed", "error", err)
		os.Exit(1)
	}
	onlineCache := onlinesvc.NewCacheService(database, online, logger)
	onlineCache.StartBackgroundRefreshWindow(rootCtx, 5*time.Minute, 10*time.Minute)
	downloads := downloadsvc.NewService(database, online, scanner, cfg.Online.DownloadsPath, logger)
	routerConfig := cfg
	routerConfig.Storage.Bookshelves = bookshelfConfigs(bookshelves)

	handler := api.NewRouter(api.Dependencies{
		Logger:      logger,
		Config:      routerConfig,
		DB:          database,
		Scanner:     scanner,
		Images:      images,
		Online:      online,
		OnlineCache: onlineCache,
		Downloads:   downloads,
	})

	httpServer := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.Server.Address)
		errCh <- httpServer.ListenAndServe()
	}()

	go func() {
		needsScan, err := needsInitialLibraryScan(rootCtx, database)
		if err != nil {
			logger.Warn("failed to inspect library cache before initial scan", "error", err)
		}
		if !needsScan {
			logger.Info("initial library scan skipped", "reason", "library cache already exists")
			return
		}

		logger.Info("initial library scan started in background")
		summary, err := scanner.Scan(rootCtx)
		if err != nil {
			if rootCtx.Err() != nil {
				logger.Info("initial library scan cancelled")
				return
			}
			logger.Error("initial library scan failed", "error", err)
			return
		}

		logger.Info("initial library scan finished",
			"bookshelves", summary.BookshelfCount,
			"manga", summary.MangaCount,
			"chapters", summary.ChapterCount,
			"pages", summary.PageCount,
		)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cancelBackground()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server shutdown complete")
}

func needsInitialLibraryScan(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookshelf`).Scan(&count); err != nil {
		return true, err
	}
	return count == 0, nil
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
	return slog.New(handler)
}

func hasBookshelfPath(bookshelves []scansvc.Bookshelf, target string) bool {
	for _, shelf := range bookshelves {
		if shelf.Path == target {
			return true
		}
	}
	return false
}

func bookshelfConfigs(bookshelves []scansvc.Bookshelf) []config.BookshelfConfig {
	configs := make([]config.BookshelfConfig, 0, len(bookshelves))
	for _, shelf := range bookshelves {
		configs = append(configs, config.BookshelfConfig{
			Name: shelf.Name,
			Path: shelf.Path,
		})
	}
	return configs
}
