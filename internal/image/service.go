package image

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	"mynewmangaui/internal/media"
)

type Service struct {
	db        *sql.DB
	cachePath string
	logger    *slog.Logger
}

func NewService(db *sql.DB, cachePath string, logger *slog.Logger) *Service {
	return &Service{
		db:        db,
		cachePath: cachePath,
		logger:    logger,
	}
}

func (s *Service) WarmCache(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id FROM manga WHERE cover_path <> '' LIMIT 24`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var mangaID string
		if err := rows.Scan(&mangaID); err != nil {
			return err
		}
		if _, err := s.EnsureMangaCoverThumb(ctx, mangaID); err != nil && s.logger != nil {
			s.logger.Warn("cover warmup failed", "manga_id", mangaID, "error", err)
		}
	}
	return rows.Err()
}

func (s *Service) EnsureMangaCoverThumb(ctx context.Context, mangaID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("image service not initialized")
	}

	var coverPath string
	var pageCount int
	if err := s.db.QueryRowContext(ctx, `SELECT cover_path, page_count FROM manga WHERE id = ?`, mangaID).Scan(&coverPath, &pageCount); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("manga %q not found", mangaID)
		}
		return "", err
	}

	if coverPath == "" && pageCount == 0 {
		return "", fmt.Errorf("manga %q has no cover source", mangaID)
	}
	if coverPath == "" {
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.path
			FROM page p
			INNER JOIN chapter c ON c.id = p.chapter_id
			WHERE c.manga_id = ?
			ORDER BY c.chapter_number ASC, c.title ASC, p.page_index ASC
			LIMIT 1
		`, mangaID).Scan(&coverPath); err != nil {
			return "", err
		}
	}

	ref, err := media.ParseRef(coverPath)
	if err != nil {
		return "", err
	}

	cacheFile := filepath.Join(s.cachePath, "covers", sanitizeFilename(mangaID)+".jpg")
	if ok, err := cacheUpToDate(cacheFile, ref.Path); err == nil && ok {
		return cacheFile, nil
	}

	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return "", err
	}

	img, err := media.Decode(coverPath)
	if err != nil {
		return "", err
	}

	thumb := resizeToWidth(img, 360)
	file, err := os.Create(cacheFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := jpeg.Encode(file, thumb, &jpeg.Options{Quality: 82}); err != nil {
		return "", err
	}
	return cacheFile, nil
}

func cacheUpToDate(cacheFile string, sourceFile string) (bool, error) {
	cacheInfo, err := os.Stat(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	sourceInfo, err := os.Stat(sourceFile)
	if err != nil {
		return false, err
	}
	return !cacheInfo.ModTime().Before(sourceInfo.ModTime()), nil
}

func resizeToWidth(src image.Image, width int) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() <= width {
		return src
	}

	height := bounds.Dy() * width / bounds.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	fillBackground(dst)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}

func fillBackground(img *image.RGBA) {
	bg := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetRGBA(x, y, bg)
		}
	}
}

func sanitizeFilename(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "|", "_")
	return replacer.Replace(value)
}
