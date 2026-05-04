package scan

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mynewmangaui/internal/media"
)

type Service struct {
	db          *sql.DB
	logger      *slog.Logger
	bookshelves []Bookshelf
	scanMu      sync.Mutex
	statusMu    sync.Mutex
	status      Status
}

type Summary struct {
	BookshelfCount int `json:"bookshelfCount"`
	MangaCount     int `json:"mangaCount"`
	ChapterCount   int `json:"chapterCount"`
	PageCount      int `json:"pageCount"`
}

type Status struct {
	Running              bool    `json:"running"`
	Scope                string  `json:"scope"`
	CurrentBookshelf     string  `json:"currentBookshelf,omitempty"`
	CompletedBookshelves int     `json:"completedBookshelves"`
	TotalBookshelves     int     `json:"totalBookshelves"`
	StartedAt            string  `json:"startedAt,omitempty"`
	FinishedAt           string  `json:"finishedAt,omitempty"`
	LastSuccessAt        string  `json:"lastSuccessAt,omitempty"`
	LastError            string  `json:"lastError,omitempty"`
	LastSummary          Summary `json:"lastSummary"`
}

type Bookshelf struct {
	Name string
	Path string
}

type bookshelfRecord struct {
	ID        string
	Name      string
	RootPath  string
	SortOrder int
	UpdatedAt time.Time
}

type mangaRecord struct {
	BookshelfID string
	ID          string
	Title       string
	TitleSort   string
	Path        string
	CoverPath   string
	UpdatedAt   time.Time
	PageCount   int
	Chapters    []chapterRecord
}

type chapterRecord struct {
	ID        string
	MangaID   string
	Title     string
	Number    *float64
	Path      string
	UpdatedAt time.Time
	PageCount int
	Pages     []pageRecord
}

type pageRecord struct {
	ID        string
	ChapterID string
	Index     int
	Path      string
	Mime      string
	Width     int
	Height    int
	SizeBytes int64
}

type archiveChapter struct {
	Title string
	Pages []media.ArchiveEntry
}

type chapterSource struct {
	Path      string
	SortName  string
	IsArchive bool
}

type directoryMetadata struct {
	Title    string                     `json:"title"`
	Cover    string                     `json:"cover"`
	Chapters []directoryMetadataChapter `json:"chapters"`
}

type directoryMetadataChapter struct {
	Title     string `json:"title"`
	Directory string `json:"directory"`
}

var chapterNumberPattern = regexp.MustCompile("(?i)(?:\u7b2c\\s*)?0*(\\d+(?:\\.\\d+)?)\\s*(?:\u8bdd|\u8a71|\u7ae0|\u5377|ch|chapter)?")
var duplicateChapterPattern = regexp.MustCompile("^((?:\u7b2c\\s*)?0*(\\d+(?:\\.\\d+)?)\\s*(?:\u8bdd|\u8a71|\u7ae0|\u5377))(?:\\s+(?:\u7b2c\\s*)?0*(\\d+(?:\\.\\d+)?)\\s*(?:\u8bdd|\u8a71|\u7ae0|\u5377))+$")

func NewService(db *sql.DB, bookshelves []Bookshelf, logger *slog.Logger) *Service {
	return &Service{db: db, bookshelves: bookshelves, logger: logger}
}

func (s *Service) Scan(ctx context.Context) (Summary, error) {
	if !s.beginScan("library") {
		return Summary{}, fmt.Errorf("scan already running")
	}
	defer func() {
		if r := recover(); r != nil {
			s.finishScan(Summary{}, fmt.Errorf("scan panicked"))
			panic(r)
		}
	}()
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if s.db == nil {
		err := fmt.Errorf("database not initialized")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	bookshelves, err := resolveBookshelves(s.bookshelves)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}
	bookshelves, err = s.mergeExistingBookshelves(ctx, bookshelves)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}
	s.setScanBookshelfProgress("", 0, len(bookshelves), Summary{})
	scanBookshelves := prioritizeBookshelvesForScan(bookshelves)

	summary := Summary{BookshelfCount: len(bookshelves)}
	if err := s.prepareLibraryScan(ctx, bookshelves); err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	for index, shelf := range scanBookshelves {
		s.setScanBookshelfProgress(shelf.Name, index, len(scanBookshelves), summary)

		manga, err := s.discoverBookshelfManga(shelf)
		if err != nil {
			s.finishScan(Summary{}, err)
			return Summary{}, err
		}
		if err := s.replaceBookshelfManga(ctx, shelf, manga); err != nil {
			s.finishScan(Summary{}, err)
			return Summary{}, err
		}
		for _, record := range manga {
			summary.MangaCount++
			summary.ChapterCount += len(record.Chapters)
			summary.PageCount += record.PageCount
		}
		s.setScanBookshelfProgress(shelf.Name, index+1, len(scanBookshelves), summary)
	}

	if err := s.removeMissingBookshelves(ctx, bookshelves); err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	if s.logger != nil {
		s.logger.Info("library scan complete",
			"manga", summary.MangaCount,
			"chapters", summary.ChapterCount,
			"pages", summary.PageCount,
			"bookshelves", summary.BookshelfCount,
		)
	}

	s.finishScan(summary, nil)
	return summary, nil
}

func (s *Service) ScanManga(ctx context.Context, mangaID string) (Summary, error) {
	if !s.beginScan("manga") {
		return Summary{}, fmt.Errorf("scan already running")
	}
	defer func() {
		if r := recover(); r != nil {
			s.finishScan(Summary{}, fmt.Errorf("scan panicked"))
			panic(r)
		}
	}()
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if s.db == nil {
		err := fmt.Errorf("database not initialized")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	summary, err := s.scanMangaByID(ctx, mangaID)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	s.finishScan(summary, nil)
	return summary, nil
}

func (s *Service) ScanTag(ctx context.Context, tagID string) (Summary, error) {
	if !s.beginScan("tag") {
		return Summary{}, fmt.Errorf("scan already running")
	}
	defer func() {
		if r := recover(); r != nil {
			s.finishScan(Summary{}, fmt.Errorf("scan panicked"))
			panic(r)
		}
	}()
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if s.db == nil {
		err := fmt.Errorf("database not initialized")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	tagID = strings.TrimSpace(tagID)
	if tagID == "" {
		err := fmt.Errorf("tag id is required")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	mangaIDs, err := s.mangaIDsForTag(ctx, tagID)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	summary := Summary{}
	s.setScanBookshelfProgress("", 0, len(mangaIDs), summary)
	for index, mangaID := range mangaIDs {
		itemSummary, err := s.scanMangaByID(ctx, mangaID)
		if err != nil {
			s.finishScan(Summary{}, err)
			return Summary{}, err
		}
		summary.MangaCount += itemSummary.MangaCount
		summary.ChapterCount += itemSummary.ChapterCount
		summary.PageCount += itemSummary.PageCount
		s.setScanBookshelfProgress("", index+1, len(mangaIDs), summary)
	}

	if len(mangaIDs) > 0 {
		summary.BookshelfCount = 1
	}
	s.finishScan(summary, nil)
	return summary, nil
}

func (s *Service) scanMangaByID(ctx context.Context, mangaID string) (Summary, error) {
	var existingPath string
	var existingBookshelfID string
	err := s.db.QueryRowContext(ctx, `SELECT path, bookshelf_id FROM manga WHERE id = ?`, mangaID).Scan(&existingPath, &existingBookshelfID)
	if err == sql.ErrNoRows {
		return Summary{}, fmt.Errorf("manga not found")
	}
	if err != nil {
		return Summary{}, fmt.Errorf("load manga path: %w", err)
	}

	record, found, err := s.discoverMangaByPath(existingBookshelfID, existingPath)
	if err != nil {
		return Summary{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("begin manga scan transaction: %w", err)
	}

	tagIDs, err := loadMangaTagIDs(ctx, tx, mangaID)
	if err != nil {
		tx.Rollback()
		return Summary{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM manga WHERE id = ?`, mangaID); err != nil {
		tx.Rollback()
		return Summary{}, fmt.Errorf("delete existing manga: %w", err)
	}

	summary := Summary{}
	if found && len(record.Chapters) > 0 {
		if err := insertManga(ctx, tx, record); err != nil {
			tx.Rollback()
			return Summary{}, err
		}
		if err := restoreMangaTags(ctx, tx, record.ID, tagIDs); err != nil {
			tx.Rollback()
			return Summary{}, err
		}
		summary.MangaCount = 1
		summary.ChapterCount = len(record.Chapters)
		summary.PageCount = record.PageCount
	}

	if err := tx.Commit(); err != nil {
		return Summary{}, fmt.Errorf("commit manga scan transaction: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("manga scan complete",
			"manga_id", mangaID,
			"found", found,
			"chapters", summary.ChapterCount,
			"pages", summary.PageCount,
		)
	}

	return summary, nil
}

func (s *Service) Status() Status {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.status
}

func (s *Service) SyncBookshelf(ctx context.Context, rootPath string) (Summary, error) {
	return s.syncBookshelf(ctx, rootPath)
}

func (s *Service) ScanBookshelf(ctx context.Context, bookshelfID string) (Summary, error) {
	if !s.beginScan("bookshelf") {
		return Summary{}, fmt.Errorf("scan already running")
	}
	defer func() {
		if r := recover(); r != nil {
			s.finishScan(Summary{}, fmt.Errorf("scan panicked"))
			panic(r)
		}
	}()
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if s.db == nil {
		err := fmt.Errorf("database not initialized")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}
	bookshelfID = strings.TrimSpace(bookshelfID)
	if bookshelfID == "" {
		err := fmt.Errorf("bookshelf id is required")
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	rootPath, err := s.bookshelfRootPath(ctx, bookshelfID)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}

	s.setScanBookshelfProgress("", 0, 1, Summary{})
	summary, err := s.syncBookshelf(ctx, rootPath)
	if err != nil {
		s.finishScan(Summary{}, err)
		return Summary{}, err
	}
	s.setScanBookshelfProgress("", 1, 1, summary)
	s.finishScan(summary, nil)
	return summary, nil
}

func (s *Service) syncBookshelf(ctx context.Context, rootPath string) (Summary, error) {
	if s.db == nil {
		return Summary{}, fmt.Errorf("database not initialized")
	}

	target := normalizeScanPath(rootPath)
	if target == "" {
		return Summary{}, fmt.Errorf("bookshelf path is required")
	}

	bookshelves, err := resolveBookshelves(s.bookshelves)
	if err != nil {
		return Summary{}, err
	}
	bookshelves, err = s.mergeExistingBookshelves(ctx, bookshelves)
	if err != nil {
		return Summary{}, err
	}

	var shelf bookshelfRecord
	found := false
	for _, item := range bookshelves {
		if normalizeScanPath(item.RootPath) == target {
			shelf = item
			found = true
			break
		}
	}
	if !found {
		dynamicShelf, err := resolveDynamicBookshelf(rootPath, len(bookshelves))
		if err != nil {
			return Summary{}, err
		}
		shelf = dynamicShelf
		found = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("begin bookshelf sync transaction: %w", err)
	}
	if err := upsertBookshelf(ctx, tx, shelf); err != nil {
		tx.Rollback()
		return Summary{}, err
	}
	if err := tx.Commit(); err != nil {
		return Summary{}, fmt.Errorf("commit bookshelf sync bootstrap: %w", err)
	}

	manga, err := s.discoverBookshelfManga(shelf)
	if err != nil {
		return Summary{}, err
	}
	if err := s.replaceBookshelfManga(ctx, shelf, manga); err != nil {
		return Summary{}, err
	}

	summary := Summary{BookshelfCount: 1}
	for _, record := range manga {
		summary.MangaCount++
		summary.ChapterCount += len(record.Chapters)
		summary.PageCount += record.PageCount
	}

	if s.logger != nil {
		s.logger.Info("bookshelf sync complete",
			"bookshelf", shelf.Name,
			"path", shelf.RootPath,
			"manga", summary.MangaCount,
			"chapters", summary.ChapterCount,
			"pages", summary.PageCount,
		)
	}

	return summary, nil
}

func (s *Service) bookshelfRootPath(ctx context.Context, bookshelfID string) (string, error) {
	var rootPath string
	err := s.db.QueryRowContext(ctx, `SELECT root_path FROM bookshelf WHERE id = ?`, bookshelfID).Scan(&rootPath)
	if err == nil {
		return rootPath, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("load bookshelf path: %w", err)
	}

	bookshelves, resolveErr := resolveBookshelves(s.bookshelves)
	if resolveErr != nil {
		return "", resolveErr
	}
	for _, shelf := range bookshelves {
		if shelf.ID == bookshelfID {
			return shelf.RootPath, nil
		}
	}

	return "", fmt.Errorf("bookshelf not found")
}

func (s *Service) mangaIDsForTag(ctx context.Context, tagID string) ([]string, error) {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tag WHERE id = ?`, tagID).Scan(&found)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tag not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load tag: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id
		FROM manga m
		JOIN manga_tag mt ON mt.manga_id = m.id
		WHERE mt.tag_id = ?
		ORDER BY m.updated_at DESC, m.title_sort ASC, m.id ASC
	`, tagID)
	if err != nil {
		return nil, fmt.Errorf("load tagged manga: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tagged manga: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tagged manga: %w", err)
	}
	return ids, nil
}

func (s *Service) beginScan(scope string) bool {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.status.Running {
		return false
	}
	s.status.Running = true
	s.status.Scope = scope
	s.status.CurrentBookshelf = ""
	s.status.CompletedBookshelves = 0
	s.status.TotalBookshelves = 0
	s.status.StartedAt = time.Now().UTC().Format(time.RFC3339)
	s.status.FinishedAt = ""
	s.status.LastError = ""
	return true
}

func (s *Service) finishScan(summary Summary, err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status.Running = false
	s.status.CurrentBookshelf = ""
	s.status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		s.status.LastError = err.Error()
		return
	}
	s.status.LastError = ""
	s.status.LastSummary = summary
	s.status.LastSuccessAt = s.status.FinishedAt
}

func (s *Service) discoverBookshelfManga(shelf bookshelfRecord) ([]mangaRecord, error) {
	items := make([]mangaRecord, 0)
	entries, err := os.ReadDir(shelf.RootPath)
	if err != nil {
		return nil, fmt.Errorf("read bookshelf root %q: %w", shelf.RootPath, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return naturalLess(entries[i].Name(), entries[j].Name())
	})

	for _, entry := range entries {
		fullPath := filepath.Join(shelf.RootPath, entry.Name())
		switch {
		case entry.IsDir():
			record, err := discoverDirectoryManga(shelf.ID, fullPath)
			if err != nil {
				return nil, err
			}
			if len(record.Chapters) > 0 {
				items = append(items, record)
			}
		case media.IsArchiveFile(entry.Name()):
			record, err := discoverArchiveManga(shelf.ID, fullPath)
			if err != nil {
				return nil, err
			}
			if len(record.Chapters) > 0 {
				items = append(items, record)
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return naturalLess(items[i].Title, items[j].Title)
	})
	return items, nil
}

func (s *Service) discoverMangaByPath(bookshelfID string, path string) (mangaRecord, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mangaRecord{}, false, nil
		}
		return mangaRecord{}, false, fmt.Errorf("stat manga path %q: %w", path, err)
	}

	if info.IsDir() {
		record, err := discoverDirectoryManga(bookshelfID, path)
		if err != nil {
			return mangaRecord{}, false, err
		}
		return record, len(record.Chapters) > 0, nil
	}

	if media.IsArchiveFile(path) {
		record, err := discoverArchiveManga(bookshelfID, path)
		if err != nil {
			return mangaRecord{}, false, err
		}
		return record, len(record.Chapters) > 0, nil
	}

	return mangaRecord{}, false, nil
}

func discoverDirectoryManga(bookshelfID string, path string) (mangaRecord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return mangaRecord{}, fmt.Errorf("stat manga dir %q: %w", path, err)
	}

	metadata, _ := loadDirectoryMetadata(path)
	title := cleanDisplayTitle(filepath.Base(path))
	if metadata.Title != "" {
		title = cleanDisplayTitle(metadata.Title)
	}

	record := mangaRecord{
		BookshelfID: bookshelfID,
		ID:          makeID("m", path),
		Title:       title,
		TitleSort:   normalizeTitle(title),
		Path:        path,
		UpdatedAt:   info.ModTime(),
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return mangaRecord{}, fmt.Errorf("read manga dir %q: %w", path, err)
	}

	chapterSources := make([]chapterSource, 0)
	rootImages := make([]string, 0)
	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		switch {
		case entry.IsDir():
			chapterSources = append(chapterSources, chapterSource{
				Path:     fullPath,
				SortName: entry.Name(),
			})
		case media.IsArchiveFile(entry.Name()):
			chapterSources = append(chapterSources, chapterSource{
				Path:      fullPath,
				SortName:  strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
				IsArchive: true,
			})
		case media.IsImageFile(entry.Name()):
			rootImages = append(rootImages, fullPath)
		}
	}

	sort.Slice(chapterSources, func(i, j int) bool {
		return naturalLess(chapterSources[i].SortName, chapterSources[j].SortName)
	})
	sort.Slice(rootImages, func(i, j int) bool {
		return naturalLess(filepath.Base(rootImages[i]), filepath.Base(rootImages[j]))
	})

	record.CoverPath = detectCover(rootImages)
	if metadata.Cover != "" {
		coverPath := filepath.Join(path, metadata.Cover)
		if _, err := os.Stat(coverPath); err == nil {
			record.CoverPath = media.FileRef(coverPath)
		}
	}

	chapterTitles := make(map[string]string, len(metadata.Chapters))
	for _, chapter := range metadata.Chapters {
		dirName := strings.TrimSpace(filepath.Base(chapter.Directory))
		chapterTitle := cleanDisplayTitle(chapter.Title)
		if dirName == "" || chapterTitle == "" {
			continue
		}
		chapterTitles[dirName] = chapterTitle
	}

	for _, source := range chapterSources {
		var (
			chapter chapterRecord
			err     error
		)
		if source.IsArchive {
			chapter, err = discoverArchiveChapter(record.ID, record.Title, source.Path)
		} else {
			chapter, err = discoverDirectoryChapter(record.ID, record.Title, source.Path)
		}
		if err != nil {
			return mangaRecord{}, err
		}
		if title := chapterTitles[filepath.Base(source.Path)]; title != "" {
			chapter.Title = title
			chapter.Number = parseChapterNumber(title)
		}
		if len(chapter.Pages) == 0 {
			continue
		}
		record.Chapters = append(record.Chapters, chapter)
		record.PageCount += chapter.PageCount
		if chapter.UpdatedAt.After(record.UpdatedAt) {
			record.UpdatedAt = chapter.UpdatedAt
		}
	}

	if len(record.Chapters) == 0 {
		chapter, err := buildPagesChapter(record.ID, record.Title, path, rootImages)
		if err != nil {
			return mangaRecord{}, err
		}
		if len(chapter.Pages) > 0 {
			record.Chapters = append(record.Chapters, chapter)
			record.PageCount = chapter.PageCount
			record.UpdatedAt = maxTime(record.UpdatedAt, chapter.UpdatedAt)
		}
	}

	if record.CoverPath == "" && len(record.Chapters) > 0 && len(record.Chapters[0].Pages) > 0 {
		record.CoverPath = record.Chapters[0].Pages[0].Path
	}

	return record, nil
}

func loadDirectoryMetadata(path string) (directoryMetadata, error) {
	payload, err := os.ReadFile(filepath.Join(path, "metadata.json"))
	if err != nil {
		return directoryMetadata{}, err
	}

	var metadata directoryMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return directoryMetadata{}, fmt.Errorf("parse metadata %q: %w", path, err)
	}

	return metadata, nil
}

func discoverDirectoryChapter(mangaID string, mangaTitle string, path string) (chapterRecord, error) {
	images, err := collectImages(path)
	if err != nil {
		return chapterRecord{}, err
	}
	return buildPagesChapter(mangaID, normalizeChapterDisplayTitle(filepath.Base(path), mangaTitle), path, images)
}

func discoverArchiveChapter(mangaID string, mangaTitle string, path string) (chapterRecord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return chapterRecord{}, fmt.Errorf("stat chapter archive %q: %w", path, err)
	}

	entries, err := media.ListArchiveImages(path)
	if err != nil {
		return chapterRecord{}, fmt.Errorf("read chapter archive %q: %w", path, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return naturalLess(entries[i].Name, entries[j].Name)
	})

	title := normalizeChapterDisplayTitle(filepath.Base(path), mangaTitle)
	record := chapterRecord{
		ID:        makeID("c", path),
		MangaID:   mangaID,
		Title:     title,
		Number:    parseChapterNumber(title),
		Path:      path,
		UpdatedAt: info.ModTime(),
	}

	archiveKind := media.ArchiveKind(path)
	for index, entry := range entries {
		page, updatedAt, err := buildArchivePage(record.ID, index, archiveKind, path, entry)
		if err != nil {
			return chapterRecord{}, err
		}
		record.Pages = append(record.Pages, page)
		record.PageCount++
		record.UpdatedAt = maxTime(record.UpdatedAt, updatedAt)
	}

	return record, nil
}

func buildPagesChapter(mangaID string, title string, logicalPath string, imagePaths []string) (chapterRecord, error) {
	number := parseChapterNumber(title)
	record := chapterRecord{
		ID:      makeID("c", logicalPath),
		MangaID: mangaID,
		Title:   title,
		Number:  number,
		Path:    logicalPath,
	}

	for index, imagePath := range imagePaths {
		page, updatedAt, err := buildFilePage(record.ID, index, imagePath)
		if err != nil {
			return chapterRecord{}, err
		}
		record.Pages = append(record.Pages, page)
		record.PageCount++
		record.UpdatedAt = maxTime(record.UpdatedAt, updatedAt)
	}
	return record, nil
}

func discoverArchiveManga(bookshelfID string, path string) (mangaRecord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return mangaRecord{}, fmt.Errorf("stat archive %q: %w", path, err)
	}

	record := mangaRecord{
		BookshelfID: bookshelfID,
		ID:          makeID("m", path),
		Title:       cleanDisplayTitle(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
		TitleSort:   normalizeTitle(cleanDisplayTitle(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))),
		Path:        path,
		UpdatedAt:   info.ModTime(),
	}

	entries, err := media.ListArchiveImages(path)
	if err != nil {
		return mangaRecord{}, fmt.Errorf("read archive %q: %w", path, err)
	}

	chapterMap := make(map[string]*archiveChapter)
	order := make([]string, 0)
	defaultTitle := record.Title
	archiveKind := media.ArchiveKind(path)

	for _, entry := range entries {
		key, title := archiveChapterKey(entry.Name, defaultTitle)
		if _, ok := chapterMap[key]; !ok {
			chapterMap[key] = &archiveChapter{Title: title}
			order = append(order, key)
		}
		chapterMap[key].Pages = append(chapterMap[key].Pages, entry)
	}

	sort.Slice(order, func(i, j int) bool {
		return naturalLess(order[i], order[j])
	})

	for _, key := range order {
		chapterData := chapterMap[key]
		sort.Slice(chapterData.Pages, func(i, j int) bool {
			return naturalLess(chapterData.Pages[i].Name, chapterData.Pages[j].Name)
		})

		chapter := chapterRecord{
			ID:      makeID("c", path+"|"+key),
			MangaID: record.ID,
			Title:   normalizeChapterDisplayTitle(chapterData.Title, record.Title),
			Number:  parseChapterNumber(normalizeChapterDisplayTitle(chapterData.Title, record.Title)),
			Path:    path + "|" + key,
		}

		for index, pageEntry := range chapterData.Pages {
			page, updatedAt, err := buildArchivePage(chapter.ID, index, archiveKind, path, pageEntry)
			if err != nil {
				return mangaRecord{}, err
			}
			chapter.Pages = append(chapter.Pages, page)
			chapter.PageCount++
			chapter.UpdatedAt = maxTime(chapter.UpdatedAt, updatedAt)
		}

		if len(chapter.Pages) == 0 {
			continue
		}
		record.Chapters = append(record.Chapters, chapter)
		record.PageCount += chapter.PageCount
		record.UpdatedAt = maxTime(record.UpdatedAt, chapter.UpdatedAt)
	}

	if len(record.Chapters) > 0 && len(record.Chapters[0].Pages) > 0 {
		record.CoverPath = record.Chapters[0].Pages[0].Path
	}

	return record, nil
}

func buildFilePage(chapterID string, index int, path string) (pageRecord, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return pageRecord{}, time.Time{}, fmt.Errorf("stat image %q: %w", path, err)
	}

	width, height := readDimensions(media.FileRef(path))
	return pageRecord{
		ID:        makeID("p", path),
		ChapterID: chapterID,
		Index:     index,
		Path:      media.FileRef(path),
		Mime:      media.GuessMime(path),
		Width:     width,
		Height:    height,
		SizeBytes: info.Size(),
	}, info.ModTime(), nil
}

func buildArchivePage(chapterID string, index int, kind string, archivePath string, entry media.ArchiveEntry) (pageRecord, time.Time, error) {
	ref := media.ArchiveRef(kind, archivePath, entry.Name)
	width, height := readDimensions(ref)
	return pageRecord{
		ID:        makeID("p", archivePath+"|"+entry.Name),
		ChapterID: chapterID,
		Index:     index,
		Path:      ref,
		Mime:      media.GuessMime(entry.Name),
		Width:     width,
		Height:    height,
		SizeBytes: entry.Size,
	}, entry.ModifiedTime, nil
}

func collectImages(root string) ([]string, error) {
	items := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if media.IsImageFile(entry.Name()) {
			items = append(items, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan chapter dir %q: %w", root, err)
	}

	sort.Slice(items, func(i, j int) bool {
		return naturalLess(items[i], items[j])
	})
	return items, nil
}

func detectCover(imagePaths []string) string {
	if len(imagePaths) == 0 {
		return ""
	}
	for _, imagePath := range imagePaths {
		base := strings.ToLower(filepath.Base(imagePath))
		if strings.HasPrefix(base, "cover.") || strings.HasPrefix(base, "folder.") || strings.HasPrefix(base, "front.") {
			return media.FileRef(imagePath)
		}
	}
	return media.FileRef(imagePaths[0])
}

func archiveChapterKey(entryName string, fallback string) (string, string) {
	clean := filepath.ToSlash(strings.TrimSpace(entryName))
	parts := strings.Split(clean, "/")
	if len(parts) > 1 {
		return parts[0], cleanDisplayTitle(parts[0])
	}
	return fallback, cleanDisplayTitle(fallback)
}

func cleanDisplayTitle(raw string) string {
	title := strings.TrimSpace(strings.TrimSuffix(raw, filepath.Ext(raw)))
	if title == "" {
		return strings.TrimSpace(raw)
	}

	replacer := strings.NewReplacer("_", " ", ".", " ")
	title = replacer.Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return strings.TrimSpace(raw)
	}
	return title
}

func cleanChapterTitle(raw string, mangaTitle string) string {
	title := cleanDisplayTitle(raw)
	if title == "" {
		return title
	}

	normalizedTitle := normalizeTitle(title)
	normalizedManga := normalizeTitle(mangaTitle)
	if normalizedManga != "" && strings.HasPrefix(normalizedTitle, normalizedManga) && strings.HasPrefix(title, mangaTitle) {
		trimmed := strings.TrimSpace(title[len(mangaTitle):])
		trimmed = strings.TrimLeft(trimmed, "-_ 銆€")
		if trimmed != "" {
			title = strings.Join(strings.Fields(trimmed), " ")
		}
	}

	if matches := duplicateChapterPattern.FindStringSubmatch(title); len(matches) >= 4 {
		left := strings.TrimLeft(matches[2], "0")
		right := strings.TrimLeft(matches[3], "0")
		if left == "" {
			left = "0"
		}
		if right == "" {
			right = "0"
		}
		if left == right {
			title = fmt.Sprintf("\u7b2c%s\u8bdd", left)
		}
	}

	if title == "" {
		return cleanDisplayTitle(raw)
	}
	return title
}

func normalizeChapterDisplayTitle(raw string, mangaTitle string) string {
	title := cleanDisplayTitle(raw)
	if title == "" {
		return title
	}

	if mangaTitle != "" && strings.HasPrefix(title, mangaTitle) {
		trimmed := strings.TrimSpace(title[len(mangaTitle):])
		trimmed = strings.TrimLeft(trimmed, "-_ .銆偮枫兓:锛?锛?\\|~!锛?锛?\" 銆€")
		if trimmed != "" {
			title = strings.Join(strings.Fields(trimmed), " ")
		}
	}

	if matches := duplicateChapterPattern.FindStringSubmatch(title); len(matches) >= 4 {
		left := strings.TrimLeft(matches[2], "0")
		right := strings.TrimLeft(matches[3], "0")
		if left == "" {
			left = "0"
		}
		if right == "" {
			right = "0"
		}
		if left == right {
			title = fmt.Sprintf("\u7b2c%s\u8bdd", left)
		}
	}

	title = strings.TrimSpace(strings.TrimLeft(title, "-_ .銆偮枫兓:锛?锛?\\|~!锛?锛?\" 銆€"))
	if title == "" {
		return cleanDisplayTitle(raw)
	}
	return title
}

func readDimensions(ref string) (int, int) {
	cfg, err := media.DecodeConfig(ref)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func parseChapterNumber(title string) *float64 {
	matches := chapterNumberPattern.FindStringSubmatch(title)
	if len(matches) < 2 {
		return nil
	}
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return nil
	}
	return &value
}

func normalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

func (s *Service) setScanBookshelfProgress(name string, completed int, total int, summary Summary) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status.CurrentBookshelf = name
	s.status.CompletedBookshelves = completed
	s.status.TotalBookshelves = total
	s.status.LastSummary = summary
}

func normalizeScanPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	cleaned = strings.ReplaceAll(cleaned, "/", `\`)
	return strings.ToLower(cleaned)
}

func resolveBookshelves(items []Bookshelf) ([]bookshelfRecord, error) {
	resolved := make([]bookshelfRecord, 0, len(items))
	for index, shelf := range items {
		name := strings.TrimSpace(shelf.Name)
		root := strings.TrimSpace(shelf.Path)
		if name == "" || root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve bookshelf root %q: %w", root, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat bookshelf root %q: %w", abs, err)
		}
		if info.IsDir() {
			resolved = append(resolved, bookshelfRecord{
				ID:        makeID("bs", abs),
				Name:      name,
				RootPath:  abs,
				SortOrder: index,
				UpdatedAt: info.ModTime(),
			})
		}
	}
	return resolved, nil
}

func (s *Service) mergeExistingBookshelves(ctx context.Context, configured []bookshelfRecord) ([]bookshelfRecord, error) {
	if s == nil || s.db == nil {
		return configured, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, root_path, sort_order, COALESCE(updated_at, '')
		FROM bookshelf
		ORDER BY sort_order ASC, name ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("load existing bookshelves: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]struct{}, len(configured))
	for _, shelf := range configured {
		seen[normalizeScanPath(shelf.RootPath)] = struct{}{}
	}

	merged := append([]bookshelfRecord(nil), configured...)
	for rows.Next() {
		var shelf bookshelfRecord
		var updatedRaw string
		if err := rows.Scan(&shelf.ID, &shelf.Name, &shelf.RootPath, &shelf.SortOrder, &updatedRaw); err != nil {
			return nil, fmt.Errorf("scan existing bookshelf: %w", err)
		}
		target := normalizeScanPath(shelf.RootPath)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		info, err := os.Stat(shelf.RootPath)
		if err != nil || !info.IsDir() {
			continue
		}
		if shelf.ID == "" {
			shelf.ID = makeID("bs", shelf.RootPath)
		}
		if strings.TrimSpace(shelf.Name) == "" {
			shelf.Name = dynamicBookshelfName(shelf.RootPath)
		}
		shelf.UpdatedAt = info.ModTime()
		merged = append(merged, shelf)
		seen[target] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing bookshelves: %w", err)
	}
	return merged, nil
}

func resolveDynamicBookshelf(rootPath string, sortOrder int) (bookshelfRecord, error) {
	abs, err := filepath.Abs(strings.TrimSpace(rootPath))
	if err != nil {
		return bookshelfRecord{}, fmt.Errorf("resolve bookshelf root %q: %w", rootPath, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return bookshelfRecord{}, fmt.Errorf("stat bookshelf root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return bookshelfRecord{}, fmt.Errorf("bookshelf root %q is not a directory", abs)
	}
	return bookshelfRecord{
		ID:        makeID("bs", abs),
		Name:      dynamicBookshelfName(abs),
		RootPath:  abs,
		SortOrder: sortOrder,
		UpdatedAt: info.ModTime(),
	}, nil
}

func dynamicBookshelfName(rootPath string) string {
	name := strings.TrimSpace(filepath.Base(rootPath))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "鍦ㄧ嚎涓嬭浇"
	}
	return "鍦ㄧ嚎涓嬭浇 路 " + name
}

func prioritizeBookshelvesForScan(items []bookshelfRecord) []bookshelfRecord {
	prioritized := append([]bookshelfRecord(nil), items...)
	sort.SliceStable(prioritized, func(i, j int) bool {
		leftOnline := strings.HasPrefix(strings.TrimSpace(prioritized[i].Name), "鍦ㄧ嚎涓嬭浇")
		rightOnline := strings.HasPrefix(strings.TrimSpace(prioritized[j].Name), "鍦ㄧ嚎涓嬭浇")
		if leftOnline == rightOnline {
			return false
		}
		return leftOnline && !rightOnline
	})
	return prioritized
}

func clearLibrary(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		`DELETE FROM page`,
		`DELETE FROM chapter`,
		`DELETE FROM manga`,
		`DELETE FROM bookshelf`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("clear existing library: %w", err)
		}
	}
	return nil
}

func (s *Service) prepareLibraryScan(ctx context.Context, bookshelves []bookshelfRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scan transaction: %w", err)
	}

	for _, shelf := range bookshelves {
		if err := upsertBookshelf(ctx, tx, shelf); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := cleanupMissingBookshelves(ctx, tx, bookshelves); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan bootstrap: %w", err)
	}
	return nil
}

func (s *Service) removeMissingBookshelves(ctx context.Context, bookshelves []bookshelfRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cleanup transaction: %w", err)
	}

	if err := cleanupMissingBookshelves(ctx, tx, bookshelves); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cleanup transaction: %w", err)
	}
	return nil
}

func cleanupMissingBookshelves(ctx context.Context, tx *sql.Tx, bookshelves []bookshelfRecord) error {
	ids := make([]string, 0, len(bookshelves))
	for _, shelf := range bookshelves {
		ids = append(ids, shelf.ID)
	}

	deleteMangaQuery := `DELETE FROM manga`
	deleteBookshelfQuery := `DELETE FROM bookshelf`
	args := make([]any, 0, len(ids))
	if len(ids) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		deleteMangaQuery += ` WHERE bookshelf_id NOT IN (` + placeholders + `)`
		deleteBookshelfQuery += ` WHERE id NOT IN (` + placeholders + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}

	if _, err := tx.ExecContext(ctx, deleteMangaQuery, args...); err != nil {
		return fmt.Errorf("cleanup removed manga: %w", err)
	}

	if _, err := tx.ExecContext(ctx, deleteBookshelfQuery, args...); err != nil {
		return fmt.Errorf("cleanup removed bookshelves: %w", err)
	}

	return nil
}

func (s *Service) replaceBookshelfManga(ctx context.Context, shelf bookshelfRecord, manga []mangaRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bookshelf transaction: %w", err)
	}

	tagsByMangaID, err := loadBookshelfMangaTagIDs(ctx, tx, shelf.ID)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := clearBookshelfManga(ctx, tx, shelf.ID); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear bookshelf %q: %w", shelf.Name, err)
	}

	for _, record := range manga {
		if err := insertManga(ctx, tx, record); err != nil {
			tx.Rollback()
			return err
		}
		if err := restoreMangaTags(ctx, tx, record.ID, tagsByMangaID[record.ID]); err != nil {
			tx.Rollback()
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE bookshelf
		SET updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, shelf.ID); err != nil {
		tx.Rollback()
		return fmt.Errorf("touch bookshelf %q: %w", shelf.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bookshelf %q: %w", shelf.Name, err)
	}
	return nil
}

func clearBookshelfManga(ctx context.Context, tx *sql.Tx, bookshelfID string) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM page
		WHERE chapter_id IN (
			SELECT c.id
			FROM chapter c
			JOIN manga m ON m.id = c.manga_id
			WHERE m.bookshelf_id = ?
		)
	`, bookshelfID); err != nil {
		return fmt.Errorf("clear bookshelf pages: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM chapter
		WHERE manga_id IN (
			SELECT id
			FROM manga
			WHERE bookshelf_id = ?
		)
	`, bookshelfID); err != nil {
		return fmt.Errorf("clear bookshelf chapters: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM manga_tag
		WHERE manga_id IN (
			SELECT id
			FROM manga
			WHERE bookshelf_id = ?
		)
	`, bookshelfID); err != nil {
		return fmt.Errorf("clear bookshelf tags: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM manga WHERE bookshelf_id = ?`, bookshelfID); err != nil {
		return fmt.Errorf("clear bookshelf manga: %w", err)
	}
	return nil
}

func loadMangaTagIDs(ctx context.Context, tx *sql.Tx, mangaID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT tag_id
		FROM manga_tag
		WHERE manga_id = ?
		ORDER BY tag_id ASC
	`, mangaID)
	if err != nil {
		return nil, fmt.Errorf("load manga tags: %w", err)
	}
	defer rows.Close()

	tagIDs := make([]string, 0)
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			return nil, fmt.Errorf("scan manga tag: %w", err)
		}
		tagIDs = append(tagIDs, tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate manga tags: %w", err)
	}
	return tagIDs, nil
}

func loadBookshelfMangaTagIDs(ctx context.Context, tx *sql.Tx, bookshelfID string) (map[string][]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT mt.manga_id, mt.tag_id
		FROM manga_tag mt
		JOIN manga m ON m.id = mt.manga_id
		WHERE m.bookshelf_id = ?
		ORDER BY mt.manga_id ASC, mt.tag_id ASC
	`, bookshelfID)
	if err != nil {
		return nil, fmt.Errorf("load bookshelf manga tags: %w", err)
	}
	defer rows.Close()

	items := make(map[string][]string)
	for rows.Next() {
		var mangaID string
		var tagID string
		if err := rows.Scan(&mangaID, &tagID); err != nil {
			return nil, fmt.Errorf("scan bookshelf manga tag: %w", err)
		}
		items[mangaID] = append(items[mangaID], tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bookshelf manga tags: %w", err)
	}
	return items, nil
}

func restoreMangaTags(ctx context.Context, tx *sql.Tx, mangaID string, tagIDs []string) error {
	for _, tagID := range tagIDs {
		if strings.TrimSpace(tagID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO manga_tag(manga_id, tag_id)
			VALUES(?, ?)
		`, mangaID, tagID); err != nil {
			return fmt.Errorf("restore manga tag: %w", err)
		}
	}
	return nil
}

func insertManga(ctx context.Context, tx *sql.Tx, record mangaRecord) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manga(id, bookshelf_id, title, title_sort, path, cover_path, page_count, created_at, updated_at, last_scan_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP)
	`,
		record.ID,
		record.BookshelfID,
		record.Title,
		record.TitleSort,
		record.Path,
		record.CoverPath,
		record.PageCount,
		sqliteTime(record.UpdatedAt),
	); err != nil {
		return fmt.Errorf("insert manga %q: %w", record.Title, err)
	}

	for _, chapter := range record.Chapters {
		if err := insertChapter(ctx, tx, chapter); err != nil {
			return err
		}
	}
	return nil
}

func upsertBookshelf(ctx context.Context, tx *sql.Tx, record bookshelfRecord) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bookshelf(id, name, root_path, sort_order, created_at, updated_at)
		VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			root_path = excluded.root_path,
			sort_order = excluded.sort_order,
			updated_at = excluded.updated_at
	`,
		record.ID,
		record.Name,
		record.RootPath,
		record.SortOrder,
		sqliteTime(record.UpdatedAt),
	); err != nil {
		return fmt.Errorf("insert bookshelf %q: %w", record.Name, err)
	}
	return nil
}

func insertChapter(ctx context.Context, tx *sql.Tx, record chapterRecord) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chapter(id, manga_id, title, chapter_number, path, page_count, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`,
		record.ID,
		record.MangaID,
		record.Title,
		record.Number,
		record.Path,
		record.PageCount,
		sqliteTime(record.UpdatedAt),
	); err != nil {
		return fmt.Errorf("insert chapter %q: %w", record.Title, err)
	}

	for _, page := range record.Pages {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO page(id, chapter_id, page_index, path, width, height, mime, size_bytes, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		`,
			page.ID,
			page.ChapterID,
			page.Index,
			page.Path,
			page.Width,
			page.Height,
			page.Mime,
			page.SizeBytes,
		); err != nil {
			return fmt.Errorf("insert page %q: %w", page.Path, err)
		}
	}
	return nil
}

func sqliteTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Format("2006-01-02 15:04:05")
}

func makeID(prefix string, raw string) string {
	sum := sha1.Sum([]byte(raw))
	return prefix + "_" + hex.EncodeToString(sum[:8])
}

func maxTime(left time.Time, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func naturalLess(left string, right string) bool {
	leftParts := tokenizeNatural(left)
	rightParts := tokenizeNatural(right)
	for i := 0; i < len(leftParts) && i < len(rightParts); i++ {
		if leftParts[i] == rightParts[i] {
			continue
		}

		leftNumber, leftErr := strconv.Atoi(leftParts[i])
		rightNumber, rightErr := strconv.Atoi(rightParts[i])
		if leftErr == nil && rightErr == nil {
			return leftNumber < rightNumber
		}
		return leftParts[i] < rightParts[i]
	}
	return len(leftParts) < len(rightParts)
}

func tokenizeNatural(value string) []string {
	value = strings.ToLower(value)
	parts := make([]string, 0)
	var current strings.Builder
	isDigit := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}

	for _, r := range value {
		digit := r >= '0' && r <= '9'
		if current.Len() == 0 {
			isDigit = digit
			current.WriteRune(r)
			continue
		}
		if digit != isDigit {
			flush()
			isDigit = digit
		}
		current.WriteRune(r)
	}
	flush()
	return parts
}
