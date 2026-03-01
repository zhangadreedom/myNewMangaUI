package scan

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mynewmangaui/internal/config"
)

var chapterNumberPattern = regexp.MustCompile(`(?i)(?:ch(?:apter)?\s*)?(\d+(?:\.\d+)?)`)

type Service struct {
	logger       *slog.Logger
	db           *sql.DB
	libraryRoots []string
}

type chapterMeta struct {
	path      string
	title     string
	number    sql.NullFloat64
	pageCount int
	mtime     time.Time
}

type pageMeta struct {
	id        string
	path      string
	index     int
	mime      string
	width     sql.NullInt64
	height    sql.NullInt64
	size      int64
	fileMTime time.Time
}

func NewService(logger *slog.Logger, db *sql.DB, cfg config.Config) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:       logger,
		db:           db,
		libraryRoots: cfg.Storage.LibraryRoots,
	}
}

func (s *Service) StartBackground(ctx context.Context) {
	go func() {
		if err := s.Scan(ctx); err != nil && !isContextDone(ctx, err) {
			s.logger.Error("background scan failed", "error", err)
		}
	}()
}

func (s *Service) Scan(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("scan service db is nil")
	}
	if len(s.libraryRoots) == 0 {
		s.logger.Info("scan skipped: no library roots configured")
		return nil
	}

	start := time.Now()
	mangaSeen := 0
	chaptersSeen := 0
	pagesSeen := 0

	for _, root := range s.libraryRoots {
		if err := ctx.Err(); err != nil {
			return err
		}

		root = filepath.Clean(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			s.logger.Warn("unable to read library root", "root", root, "error", err)
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}

			mangaPath := filepath.Join(root, entry.Name())
			chapterDirs, err := readChapterDirs(mangaPath)
			if err != nil {
				s.logger.Warn("unable to read manga folder", "mangaPath", mangaPath, "error", err)
				continue
			}
			if len(chapterDirs) == 0 {
				continue
			}

			mangaID := stableID("manga", mangaPath)
			mangaTitle := entry.Name()

			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("begin tx for manga %s: %w", mangaPath, err)
			}

			if err := upsertManga(ctx, tx, mangaID, mangaTitle, mangaPath); err != nil {
				tx.Rollback()
				return fmt.Errorf("upsert manga %s: %w", mangaPath, err)
			}

			chapterCount := 0
			mangaPageCount := 0
			var mangaMTime time.Time

			for _, chapterDir := range chapterDirs {
				chapterPath := filepath.Join(mangaPath, chapterDir.Name())
				chMeta, pages, err := collectChapter(ctx, chapterPath)
				if err != nil {
					s.logger.Warn("unable to process chapter", "chapterPath", chapterPath, "error", err)
					continue
				}
				if chMeta.pageCount == 0 {
					continue
				}

				chapterID := stableID("chapter", chapterPath)
				if err := upsertChapter(ctx, tx, chapterID, mangaID, chMeta); err != nil {
					tx.Rollback()
					return fmt.Errorf("upsert chapter %s: %w", chapterPath, err)
				}
				if err := upsertPages(ctx, tx, chapterID, pages); err != nil {
					tx.Rollback()
					return fmt.Errorf("upsert pages %s: %w", chapterPath, err)
				}

				chapterCount++
				chaptersSeen++
				mangaPageCount += chMeta.pageCount
				pagesSeen += chMeta.pageCount
				if chMeta.mtime.After(mangaMTime) {
					mangaMTime = chMeta.mtime
				}
			}

			if chapterCount == 0 {
				tx.Rollback()
				continue
			}

			if err := finalizeMangaScan(ctx, tx, mangaID, chapterCount, mangaPageCount, mangaMTime); err != nil {
				tx.Rollback()
				return fmt.Errorf("finalize manga %s: %w", mangaPath, err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit tx for manga %s: %w", mangaPath, err)
			}

			mangaSeen++
		}
	}

	s.logger.Info("scan complete",
		"manga", mangaSeen,
		"chapters", chaptersSeen,
		"pages", pagesSeen,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

func readChapterDirs(mangaPath string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(mangaPath)
	if err != nil {
		return nil, err
	}
	dirs := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name())
	})
	return dirs, nil
}

func collectChapter(ctx context.Context, chapterPath string) (chapterMeta, []pageMeta, error) {
	var pages []pageMeta
	walkErr := filepath.WalkDir(chapterPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !isImageFile(path) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		pages = append(pages, pageMeta{
			id:        stableID("page", path),
			path:      path,
			mime:      mimeFromExt(filepath.Ext(path)),
			size:      info.Size(),
			fileMTime: info.ModTime().UTC(),
		})
		return nil
	})
	if walkErr != nil {
		return chapterMeta{}, nil, walkErr
	}

	sort.Slice(pages, func(i, j int) bool {
		return naturalLess(filepath.Base(pages[i].path), filepath.Base(pages[j].path))
	})

	for i := range pages {
		pages[i].index = i
	}

	info, err := os.Stat(chapterPath)
	if err != nil {
		return chapterMeta{}, nil, err
	}

	chapterName := filepath.Base(chapterPath)
	meta := chapterMeta{
		path:      chapterPath,
		title:     chapterName,
		number:    parseChapterNumber(chapterName),
		pageCount: len(pages),
		mtime:     info.ModTime().UTC(),
	}

	return meta, pages, nil
}

func upsertManga(ctx context.Context, tx *sql.Tx, id, title, path string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO manga (id, title, title_sort, path, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(path) DO UPDATE SET
			title = excluded.title,
			title_sort = excluded.title_sort,
			updated_at = CURRENT_TIMESTAMP
	`, id, title, strings.ToLower(title), path)
	return err
}

func upsertChapter(ctx context.Context, tx *sql.Tx, id, mangaID string, chapter chapterMeta) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO chapter (id, manga_id, title, number, path, page_count, file_mtime, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(path) DO UPDATE SET
			title = excluded.title,
			number = excluded.number,
			page_count = excluded.page_count,
			file_mtime = excluded.file_mtime,
			updated_at = CURRENT_TIMESTAMP
	`, id, mangaID, chapter.title, chapter.number, chapter.path, chapter.pageCount, chapter.mtime)
	return err
}

func upsertPages(ctx context.Context, tx *sql.Tx, chapterID string, pages []pageMeta) error {
	for _, page := range pages {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO page (id, chapter_id, page_index, path, mime, width, height, size_bytes, file_mtime, checksum)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				chapter_id = excluded.chapter_id,
				page_index = excluded.page_index,
				mime = excluded.mime,
				width = excluded.width,
				height = excluded.height,
				size_bytes = excluded.size_bytes,
				file_mtime = excluded.file_mtime
		`, page.id, chapterID, page.index, page.path, page.mime, page.width, page.height, page.size, page.fileMTime, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func finalizeMangaScan(ctx context.Context, tx *sql.Tx, mangaID string, chapterCount, pageCount int, mangaMTime time.Time) error {
	var lastScan any
	if !mangaMTime.IsZero() {
		lastScan = mangaMTime
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE manga
		SET chapter_count = ?, page_count = ?, last_scan_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, chapterCount, pageCount, lastScan, mangaID)
	return err
}

func parseChapterNumber(name string) sql.NullFloat64 {
	match := chapterNumberPattern.FindStringSubmatch(name)
	if len(match) < 2 {
		return sql.NullFloat64{}
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: value, Valid: true}
}

func stableID(prefix, path string) string {
	hash := sha1.Sum([]byte(filepath.Clean(path)))
	return prefix + "_" + hex.EncodeToString(hash[:])
}

func isImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func naturalLess(a, b string) bool {
	return naturalKey(a) < naturalKey(b)
}

func naturalKey(input string) string {
	var b strings.Builder
	segment := strings.Builder{}
	flushDigits := func() {
		if segment.Len() == 0 {
			return
		}
		n, err := strconv.Atoi(segment.String())
		if err != nil {
			b.WriteString(segment.String())
		} else {
			b.WriteString(fmt.Sprintf("%08d", n))
		}
		segment.Reset()
	}

	for _, r := range strings.ToLower(input) {
		if r >= '0' && r <= '9' {
			segment.WriteRune(r)
			continue
		}
		flushDigits()
		b.WriteRune(r)
	}
	flushDigits()
	return b.String()
}

func isContextDone(ctx context.Context, err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded || ctx.Err() != nil
}
