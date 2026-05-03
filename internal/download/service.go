package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	onlinesvc "mynewmangaui/internal/online"
	scansvc "mynewmangaui/internal/scan"
)

var (
	errJobPaused   = errors.New("download job paused")
	errJobCanceled = errors.New("download job canceled")
)

type Service struct {
	db       *sql.DB
	online   *onlinesvc.Service
	scanner  *scansvc.Service
	logger   *slog.Logger
	rootPath string

	mu     sync.Mutex
	active map[string]context.CancelFunc
}

func (s *Service) sourceOutputRoot(sourceID string) string {
	return filepath.Join(s.rootPath, strings.TrimSpace(sourceID))
}

type CreateJobInput struct {
	SourceID   string
	MangaID    string
	ChapterIDs []string
	Mode       string
}

type metadataFile struct {
	SourceID string            `json:"sourceId"`
	MangaID  string            `json:"mangaId"`
	Title    string            `json:"title"`
	Author   string            `json:"author,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Cover    string            `json:"cover,omitempty"`
	Chapters []metadataChapter `json:"chapters"`
	SavedAt  string            `json:"savedAt"`
}

type metadataChapter struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Order     int    `json:"order"`
	PageCount int    `json:"pageCount"`
	Directory string `json:"directory"`
}

func NewService(db *sql.DB, online *onlinesvc.Service, scanner *scansvc.Service, rootPath string, logger *slog.Logger) *Service {
	service := &Service{
		db:       db,
		online:   online,
		scanner:  scanner,
		logger:   logger,
		rootPath: strings.TrimSpace(rootPath),
		active:   make(map[string]context.CancelFunc),
	}
	service.resumePendingJobs(context.Background())
	return service
}

func (s *Service) CreateJob(ctx context.Context, input CreateJobInput) (onlinesvc.DownloadJobDetail, bool, error) {
	if s.db == nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("database not initialized")
	}
	if s.online == nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("online service not initialized")
	}
	if strings.TrimSpace(s.rootPath) == "" {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("downloads path is not configured")
	}

	sourceID := strings.TrimSpace(input.SourceID)
	mangaID := strings.TrimSpace(input.MangaID)
	if sourceID == "" || mangaID == "" {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("source id and manga id are required")
	}

	manga, err := s.online.GetManga(ctx, sourceID, mangaID)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, false, err
	}
	chapters, err := s.online.GetChapters(ctx, sourceID, mangaID)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, false, err
	}

	selected := filterChapters(chapters, input.ChapterIDs)
	if len(selected) == 0 {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("no chapters selected")
	}

	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		if len(selected) == 1 {
			mode = "chapter"
		} else {
			mode = "manga"
		}
	}

	selectedIDs := chapterIDsFromSelection(selected)
	if existing, found, err := s.findDuplicateJob(ctx, sourceID, mangaID, selectedIDs); err != nil {
		return onlinesvc.DownloadJobDetail{}, false, err
	} else if found {
		existing.Existing = true
		return existing, true, nil
	}

	jobID := uuid.NewString()
	outputRoot := s.jobOutputRoot(sourceID, manga)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("begin download transaction: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO download_job(
			id, source_id, external_manga_id, manga_title, status, mode,
			total_chapters, done_chapters, total_pages, done_pages, failed_pages,
			concurrency, request_interval_ms, output_root, error_message
		) VALUES(?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 3, 800, ?, '')
	`, jobID, sourceID, mangaID, manga.Title, onlinesvc.DownloadJobQueued, mode, len(selected), outputRoot)
	if err != nil {
		tx.Rollback()
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("insert download job: %w", err)
	}

	for _, chapter := range selected {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO download_job_chapter(
				job_id, source_id, external_manga_id, external_chapter_id,
				title, chapter_order, page_count, done_pages, failed_pages, status
			) VALUES(?, ?, ?, ?, ?, ?, ?, 0, 0, 'queued')
		`, jobID, sourceID, mangaID, chapter.ID, chapter.Title, chapter.Order, chapter.PageCount)
		if err != nil {
			tx.Rollback()
			return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("insert job chapter %q: %w", chapter.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("commit download job: %w", err)
	}

	s.enqueue(jobID)
	item, err := s.GetJob(ctx, jobID)
	return item, false, err
}

func (s *Service) ListJobs(ctx context.Context, limit int) ([]onlinesvc.DownloadJob, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, source_id, external_manga_id, manga_title, status, mode,
			total_chapters, done_chapters, total_pages, done_pages, failed_pages,
			error_message, created_at, updated_at,
			COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM download_job
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query download jobs: %w", err)
	}
	defer rows.Close()

	items := make([]onlinesvc.DownloadJob, 0, limit)
	for rows.Next() {
		job, err := scanDownloadJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate download jobs: %w", err)
	}
	return items, nil
}

func (s *Service) GetJob(ctx context.Context, jobID string) (onlinesvc.DownloadJobDetail, error) {
	if s.db == nil {
		return onlinesvc.DownloadJobDetail{}, fmt.Errorf("database not initialized")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			id, source_id, external_manga_id, manga_title, status, mode,
			total_chapters, done_chapters, total_pages, done_pages, failed_pages,
			error_message, created_at, updated_at,
			COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM download_job
		WHERE id = ?
	`, jobID)
	job, err := scanDownloadJob(row)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			job_id, source_id, external_manga_id, external_chapter_id, title,
			chapter_order, page_count, done_pages, failed_pages, status,
			created_at, updated_at
		FROM download_job_chapter
		WHERE job_id = ?
		ORDER BY chapter_order ASC, external_chapter_id ASC
	`, jobID)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, fmt.Errorf("query download job chapters: %w", err)
	}
	defer rows.Close()

	chapters := make([]onlinesvc.DownloadChapter, 0, job.TotalChapters)
	for rows.Next() {
		var item onlinesvc.DownloadChapter
		if err := rows.Scan(
			&item.JobID,
			&item.SourceID,
			&item.MangaID,
			&item.ChapterID,
			&item.Title,
			&item.Order,
			&item.PageCount,
			&item.DonePages,
			&item.FailedPages,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return onlinesvc.DownloadJobDetail{}, fmt.Errorf("scan download chapter: %w", err)
		}
		chapters = append(chapters, item)
	}
	if err := rows.Err(); err != nil {
		return onlinesvc.DownloadJobDetail{}, fmt.Errorf("iterate download chapters: %w", err)
	}

	return onlinesvc.DownloadJobDetail{
		DownloadJob: job,
		Chapters:    chapters,
	}, nil
}

func (s *Service) PauseJob(ctx context.Context, jobID string) error {
	if err := s.updateJobStatus(ctx, jobID, onlinesvc.DownloadJobPaused, ""); err != nil {
		return err
	}
	s.stop(jobID)
	return nil
}

func (s *Service) ResumeJob(ctx context.Context, jobID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, error_message = '', finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?, ?)
	`, onlinesvc.DownloadJobQueued, jobID, onlinesvc.DownloadJobPaused, onlinesvc.DownloadJobFailed, onlinesvc.DownloadJobQueued); err != nil {
		return fmt.Errorf("resume download job: %w", err)
	}
	s.enqueue(jobID)
	return nil
}

func (s *Service) CancelJob(ctx context.Context, jobID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status NOT IN (?, ?)
	`, onlinesvc.DownloadJobCanceled, jobID, onlinesvc.DownloadJobDone, onlinesvc.DownloadJobCanceled); err != nil {
		return fmt.Errorf("cancel download job: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job_chapter
		SET status = 'canceled', updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND status IN ('queued', 'running')
	`, jobID); err != nil {
		return fmt.Errorf("cancel download job chapters: %w", err)
	}
	s.stop(jobID)
	return nil
}

func (s *Service) RetryJob(ctx context.Context, jobID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_item
		SET status = 'queued', error = '', updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND status = 'failed'
	`, jobID); err != nil {
		return fmt.Errorf("reset failed download items: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job_chapter
		SET status = 'queued', failed_pages = 0, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND status IN ('failed', 'queued')
	`, jobID); err != nil {
		return fmt.Errorf("reset failed download chapters: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, error_message = '', finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, onlinesvc.DownloadJobQueued, jobID); err != nil {
		return fmt.Errorf("reset download job: %w", err)
	}
	s.enqueue(jobID)
	return nil
}

func (s *Service) RedownloadJob(ctx context.Context, jobID string) error {
	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if s.online == nil {
		return fmt.Errorf("online service not initialized")
	}

	detail, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	manga, err := s.online.GetManga(ctx, detail.SourceID, detail.MangaID)
	if err != nil {
		return fmt.Errorf("load manga detail for redownload: %w", err)
	}

	s.stop(jobID)
	if err := s.removeJobOutput(detail.DownloadJob, manga, detail.Chapters); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin redownload transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM download_item
		WHERE job_id = ?
	`, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear download items: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE download_job_chapter
		SET page_count = 0, done_pages = 0, failed_pages = 0, status = 'queued', updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ?
	`, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("reset download chapters: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE download_job
		SET
			status = ?,
			done_chapters = 0,
			total_pages = 0,
			done_pages = 0,
			failed_pages = 0,
			error_message = '',
			started_at = NULL,
			finished_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, onlinesvc.DownloadJobQueued, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("reset download job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit redownload transaction: %w", err)
	}

	s.enqueue(jobID)
	return nil
}

func (s *Service) DeleteJobRecord(ctx context.Context, jobID string) error {
	return s.deleteJob(ctx, jobID, false)
}

func (s *Service) DeleteJobAndFiles(ctx context.Context, jobID string) error {
	return s.deleteJob(ctx, jobID, true)
}

func (s *Service) deleteJob(ctx context.Context, jobID string, removeFiles bool) error {
	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	detail, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	s.stop(jobID)

	if removeFiles {
		if err := s.deleteJobFiles(ctx, detail); err != nil {
			return err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete download transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM download_item WHERE job_id = ?`, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete download items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM download_job_chapter WHERE job_id = ?`, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete download chapters: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM download_job WHERE id = ?`, jobID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete download job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete download transaction: %w", err)
	}
	return nil
}

func (s *Service) deleteJobFiles(ctx context.Context, detail onlinesvc.DownloadJobDetail) error {
	var outputRoot string
	if err := s.db.QueryRowContext(ctx, `SELECT output_root FROM download_job WHERE id = ?`, detail.ID).Scan(&outputRoot); err != nil {
		return fmt.Errorf("load download output root: %w", err)
	}
	outputRoot = strings.TrimSpace(outputRoot)
	if outputRoot == "" {
		return nil
	}

	manga := onlinesvc.Manga{
		SourceID: detail.SourceID,
		ID:       detail.MangaID,
		Title:    detail.MangaTitle,
	}
	if s.online != nil {
		if loaded, err := s.online.GetManga(ctx, detail.SourceID, detail.MangaID); err == nil {
			manga = loaded
		}
	}

	if err := s.removeJobOutput(detail.DownloadJob, manga, detail.Chapters); err != nil {
		return err
	}

	if s.scanner != nil {
		if _, err := s.scanner.SyncBookshelf(context.Background(), s.sourceOutputRoot(detail.SourceID)); err != nil {
			s.logWarn("sync bookshelf after delete failed", "jobID", detail.ID, "sourceID", detail.SourceID, "error", err)
		}
	}
	return nil
}

func (s *Service) resumePendingJobs(ctx context.Context) {
	if s.db == nil {
		return
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE status IN (?, ?)
	`, onlinesvc.DownloadJobQueued, onlinesvc.DownloadJobRunning, onlinesvc.DownloadJobProcessing); err != nil && s.logger != nil {
		s.logger.Warn("failed to reset running download jobs", "error", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM download_job
		WHERE status = ?
		ORDER BY created_at ASC
	`, onlinesvc.DownloadJobQueued)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to load queued download jobs", "error", err)
		}
		return
	}
	defer rows.Close()

	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			continue
		}
		s.enqueue(jobID)
	}
}

func (s *Service) enqueue(jobID string) {
	s.mu.Lock()
	if _, exists := s.active[jobID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.active[jobID] = cancel
	s.mu.Unlock()

	go func() {
		defer s.finishActive(jobID)
		s.runJob(ctx, jobID)
	}()
}

func (s *Service) stop(jobID string) {
	s.mu.Lock()
	cancel := s.active[jobID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) finishActive(jobID string) {
	s.mu.Lock()
	cancel := s.active[jobID]
	delete(s.active, jobID)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) runJob(ctx context.Context, jobID string) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		s.logWarn("load download job failed", "jobID", jobID, "error", err)
		return
	}
	if job.Status == onlinesvc.DownloadJobDone || job.Status == onlinesvc.DownloadJobCanceled {
		return
	}

	if err := s.updateJobStatus(ctx, jobID, onlinesvc.DownloadJobRunning, ""); err != nil {
		s.logWarn("mark job running failed", "jobID", jobID, "error", err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET started_at = COALESCE(started_at, CURRENT_TIMESTAMP), finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, jobID); err != nil {
		s.logWarn("update job start time failed", "jobID", jobID, "error", err)
	}

	manga, err := s.online.GetManga(ctx, job.SourceID, job.MangaID)
	if err != nil {
		_ = s.failJob(ctx, jobID, fmt.Sprintf("load manga detail failed: %v", err))
		return
	}

	if err := os.MkdirAll(s.jobOutputRoot(job.SourceID, manga), 0o755); err != nil {
		_ = s.failJob(ctx, jobID, fmt.Sprintf("create output dir failed: %v", err))
		return
	}
	if err := s.persistCover(ctx, job.SourceID, manga); err != nil {
		s.logWarn("persist cover failed", "jobID", jobID, "error", err)
	}

	chapters := append([]onlinesvc.DownloadChapter(nil), job.Chapters...)
	failed := false
	for _, chapter := range chapters {
		stopErr := s.checkStop(ctx, jobID)
		if stopErr != nil {
			s.finishStoppedJob(ctx, jobID, stopErr)
			return
		}

		if _, err := s.db.ExecContext(ctx, `
			UPDATE download_job_chapter
			SET status = 'running', updated_at = CURRENT_TIMESTAMP
			WHERE job_id = ? AND external_chapter_id = ?
		`, jobID, chapter.ChapterID); err != nil {
			failed = true
			continue
		}

		if err := s.processChapter(ctx, job.DownloadJob, manga, chapter); err != nil {
			if errors.Is(err, errJobPaused) || errors.Is(err, errJobCanceled) {
				s.finishStoppedJob(ctx, jobID, err)
				return
			}
			failed = true
			if _, execErr := s.db.ExecContext(ctx, `
				UPDATE download_job_chapter
				SET status = 'failed', updated_at = CURRENT_TIMESTAMP
				WHERE job_id = ? AND external_chapter_id = ?
			`, jobID, chapter.ChapterID); execErr != nil {
				s.logWarn("mark chapter failed", "jobID", jobID, "chapterID", chapter.ChapterID, "error", execErr)
			}
		}
		if err := s.refreshJobProgress(ctx, jobID); err != nil {
			s.logWarn("refresh job progress failed", "jobID", jobID, "error", err)
		}
	}

	if err := s.updateJobStatus(ctx, jobID, onlinesvc.DownloadJobProcessing, ""); err != nil {
		s.logWarn("mark job processing failed", "jobID", jobID, "error", err)
	}

	if err := s.writeMetadata(ctx, job.DownloadJob, manga); err != nil {
		s.logWarn("write metadata failed", "jobID", jobID, "error", err)
	}

	detail, err := s.GetJob(ctx, jobID)
	if err != nil {
		_ = s.failJob(ctx, jobID, fmt.Sprintf("reload download job failed: %v", err))
		return
	}
	if detail.Status == onlinesvc.DownloadJobPaused || detail.Status == onlinesvc.DownloadJobCanceled {
		return
	}
	if failed || detail.FailedPages > 0 {
		_ = s.failJob(ctx, jobID, "some pages failed to download")
		return
	}

	if s.scanner != nil {
		if _, err := s.scanner.SyncBookshelf(context.Background(), s.sourceOutputRoot(job.SourceID)); err != nil {
			s.logWarn("sync bookshelf after download failed", "jobID", jobID, "sourceID", job.SourceID, "error", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, done_chapters = total_chapters, error_message = '', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, onlinesvc.DownloadJobDone, jobID); err != nil {
		s.logWarn("mark job done failed", "jobID", jobID, "error", err)
		return
	}
}

func (s *Service) processChapter(ctx context.Context, job onlinesvc.DownloadJob, manga onlinesvc.Manga, chapter onlinesvc.DownloadChapter) error {
	pages, err := s.online.GetPages(ctx, job.SourceID, chapter.ChapterID)
	if err != nil {
		return fmt.Errorf("load chapter pages failed: %w", err)
	}

	chapterDirName := downloadChapterDirName(chapter.Order, chapter.Title)
	chapterRoot := filepath.Join(s.jobOutputRoot(job.SourceID, manga), chapterDirName)
	if err := os.MkdirAll(chapterRoot, 0o755); err != nil {
		return fmt.Errorf("create chapter dir failed: %w", err)
	}

	for index, page := range pages {
		stopErr := s.checkStop(ctx, job.ID)
		if stopErr != nil {
			return stopErr
		}

		ext := inferFileExtension(page.RemoteURL, "")
		localPath := filepath.Join(chapterRoot, fmt.Sprintf("%03d%s", index+1, ext))
		if err := s.ensureDownloadItem(ctx, job, chapter, index, page.RemoteURL, localPath); err != nil {
			return err
		}

		var itemStatus string
		var savedPath string
		err := s.db.QueryRowContext(ctx, `
			SELECT status, local_path
			FROM download_item
			WHERE job_id = ? AND external_chapter_id = ? AND page_index = ?
		`, job.ID, chapter.ChapterID, index).Scan(&itemStatus, &savedPath)
		if err != nil {
			return fmt.Errorf("load download item state failed: %w", err)
		}
		if itemStatus == string(onlinesvc.DownloadItemDone) && savedPath != "" {
			if _, err := os.Stat(savedPath); err == nil {
				continue
			}
		}

		if _, err := s.db.ExecContext(ctx, `
			UPDATE download_item
			SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP
			WHERE job_id = ? AND external_chapter_id = ? AND page_index = ?
		`, onlinesvc.DownloadItemRunning, job.ID, chapter.ChapterID, index); err != nil {
			return fmt.Errorf("mark item running failed: %w", err)
		}

		payload, mimeType, err := s.online.FetchImage(ctx, job.SourceID, page.RemoteURL)
		if err != nil {
			if _, execErr := s.db.ExecContext(ctx, `
				UPDATE download_item
				SET status = ?, error = ?, retry_count = retry_count + 1, updated_at = CURRENT_TIMESTAMP
				WHERE job_id = ? AND external_chapter_id = ? AND page_index = ?
			`, onlinesvc.DownloadItemFailed, err.Error(), job.ID, chapter.ChapterID, index); execErr != nil {
				s.logWarn("mark item failed", "jobID", job.ID, "chapterID", chapter.ChapterID, "pageIndex", index, "error", execErr)
			}
			_ = s.refreshChapterProgress(ctx, job.ID, chapter.ChapterID)
			continue
		}

		ext = inferFileExtension(page.RemoteURL, mimeType)
		localPath = filepath.Join(chapterRoot, fmt.Sprintf("%03d%s", index+1, ext))
		if err := os.WriteFile(localPath, payload, 0o644); err != nil {
			if _, execErr := s.db.ExecContext(ctx, `
				UPDATE download_item
				SET status = ?, error = ?, retry_count = retry_count + 1, updated_at = CURRENT_TIMESTAMP
				WHERE job_id = ? AND external_chapter_id = ? AND page_index = ?
			`, onlinesvc.DownloadItemFailed, err.Error(), job.ID, chapter.ChapterID, index); execErr != nil {
				s.logWarn("mark item write failed", "jobID", job.ID, "chapterID", chapter.ChapterID, "pageIndex", index, "error", execErr)
			}
			_ = s.refreshChapterProgress(ctx, job.ID, chapter.ChapterID)
			continue
		}

		if _, err := s.db.ExecContext(ctx, `
			UPDATE download_item
			SET status = ?, local_path = ?, mime = ?, size_bytes = ?, error = '', updated_at = CURRENT_TIMESTAMP
			WHERE job_id = ? AND external_chapter_id = ? AND page_index = ?
		`, onlinesvc.DownloadItemDone, localPath, mimeType, len(payload), job.ID, chapter.ChapterID, index); err != nil {
			return fmt.Errorf("mark item done failed: %w", err)
		}
		if err := s.refreshChapterProgress(ctx, job.ID, chapter.ChapterID); err != nil {
			return err
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job_chapter
		SET status = CASE WHEN failed_pages > 0 THEN 'failed' ELSE 'done' END, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND external_chapter_id = ?
	`, job.ID, chapter.ChapterID); err != nil {
		return fmt.Errorf("mark chapter complete failed: %w", err)
	}
	return nil
}

func (s *Service) ensureDownloadItem(ctx context.Context, job onlinesvc.DownloadJob, chapter onlinesvc.DownloadChapter, pageIndex int, remoteURL string, localPath string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO download_item(
			id, job_id, source_id, external_chapter_id, page_index, remote_url,
			local_path, mime, size_bytes, status, error, retry_count
		) VALUES(?, ?, ?, ?, ?, ?, ?, '', 0, ?, '', 0)
		ON CONFLICT(job_id, external_chapter_id, page_index) DO UPDATE SET
			remote_url = excluded.remote_url,
			local_path = CASE
				WHEN download_item.local_path = '' THEN excluded.local_path
				ELSE download_item.local_path
			END,
			updated_at = CURRENT_TIMESTAMP
	`, uuid.NewString(), job.ID, job.SourceID, chapter.ChapterID, pageIndex, remoteURL, localPath, onlinesvc.DownloadItemQueued)
	if err != nil {
		return fmt.Errorf("upsert download item failed: %w", err)
	}
	return nil
}

func (s *Service) refreshChapterProgress(ctx context.Context, jobID string, chapterID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_job_chapter
		SET
			page_count = (SELECT COUNT(*) FROM download_item WHERE job_id = ? AND external_chapter_id = ?),
			done_pages = (SELECT COUNT(*) FROM download_item WHERE job_id = ? AND external_chapter_id = ? AND status = 'done'),
			failed_pages = (SELECT COUNT(*) FROM download_item WHERE job_id = ? AND external_chapter_id = ? AND status = 'failed'),
			updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND external_chapter_id = ?
	`, jobID, chapterID, jobID, chapterID, jobID, chapterID, jobID, chapterID)
	if err != nil {
		return fmt.Errorf("refresh chapter progress failed: %w", err)
	}
	return s.refreshJobProgress(ctx, jobID)
}

func (s *Service) refreshJobProgress(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET
			total_pages = (SELECT COUNT(*) FROM download_item WHERE job_id = ?),
			done_pages = (SELECT COUNT(*) FROM download_item WHERE job_id = ? AND status = 'done'),
			failed_pages = (SELECT COUNT(*) FROM download_item WHERE job_id = ? AND status = 'failed'),
			done_chapters = (SELECT COUNT(*) FROM download_job_chapter WHERE job_id = ? AND status = 'done'),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, jobID, jobID, jobID, jobID, jobID)
	if err != nil {
		return fmt.Errorf("refresh job progress failed: %w", err)
	}
	return nil
}

func (s *Service) updateJobStatus(ctx context.Context, jobID string, status onlinesvc.DownloadJobStatus, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, errorMessage, jobID)
	if err != nil {
		return fmt.Errorf("update download job status: %w", err)
	}
	return nil
}

func (s *Service) failJob(ctx context.Context, jobID string, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, error_message = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, onlinesvc.DownloadJobFailed, errorMessage, jobID)
	if err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	return nil
}

func (s *Service) finishStoppedJob(ctx context.Context, jobID string, stopErr error) {
	status := onlinesvc.DownloadJobPaused
	if errors.Is(stopErr, errJobCanceled) {
		status = onlinesvc.DownloadJobCanceled
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE download_job
		SET status = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, jobID); err != nil {
		s.logWarn("mark stopped job failed", "jobID", jobID, "error", err)
	}
}

func (s *Service) checkStop(ctx context.Context, jobID string) error {
	select {
	case <-ctx.Done():
		status, err := s.currentJobStatus(context.Background(), jobID)
		if err == nil && status == onlinesvc.DownloadJobCanceled {
			return errJobCanceled
		}
		return errJobPaused
	default:
	}

	status, err := s.currentJobStatus(ctx, jobID)
	if err != nil {
		return err
	}
	switch status {
	case onlinesvc.DownloadJobPaused:
		return errJobPaused
	case onlinesvc.DownloadJobCanceled:
		return errJobCanceled
	default:
		return nil
	}
}

func (s *Service) currentJobStatus(ctx context.Context, jobID string) (onlinesvc.DownloadJobStatus, error) {
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM download_job WHERE id = ?`, jobID).Scan(&status); err != nil {
		return "", fmt.Errorf("load download job status: %w", err)
	}
	return onlinesvc.DownloadJobStatus(status), nil
}

func (s *Service) persistCover(ctx context.Context, sourceID string, manga onlinesvc.Manga) error {
	if strings.TrimSpace(manga.CoverURL) == "" {
		return nil
	}
	payload, mimeType, err := s.online.FetchImage(ctx, sourceID, manga.CoverURL)
	if err != nil {
		return err
	}
	path := filepath.Join(s.jobOutputRoot(sourceID, manga), "cover"+inferFileExtension(manga.CoverURL, mimeType))
	return os.WriteFile(path, payload, 0o644)
}

func (s *Service) writeMetadata(ctx context.Context, job onlinesvc.DownloadJob, manga onlinesvc.Manga) error {
	detail, err := s.GetJob(ctx, job.ID)
	if err != nil {
		return err
	}

	meta := metadataFile{
		SourceID: manga.SourceID,
		MangaID:  manga.ID,
		Title:    manga.Title,
		Author:   manga.Author,
		Tags:     append([]string(nil), manga.Tags...),
		SavedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	for _, chapter := range detail.Chapters {
		meta.Chapters = append(meta.Chapters, metadataChapter{
			ID:        chapter.ChapterID,
			Title:     chapter.Title,
			Order:     chapter.Order,
			PageCount: chapter.PageCount,
			Directory: downloadChapterDirName(chapter.Order, chapter.Title),
		})
	}

	coverCandidates, _ := filepath.Glob(filepath.Join(s.jobOutputRoot(job.SourceID, manga), "cover.*"))
	if len(coverCandidates) > 0 {
		meta.Cover = filepath.Base(coverCandidates[0])
	}

	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	return os.WriteFile(filepath.Join(s.jobOutputRoot(job.SourceID, manga), "metadata.json"), payload, 0o644)
}

func (s *Service) jobOutputRoot(sourceID string, manga onlinesvc.Manga) string {
	name := sanitizeForPath(manga.Title)
	if name == "" {
		name = "manga"
	}
	return filepath.Join(s.rootPath, sanitizeForPath(sourceID), fmt.Sprintf("%s-%s", name, sanitizeForPath(manga.ID)))
}

func filterChapters(chapters []onlinesvc.Chapter, chapterIDs []string) []onlinesvc.Chapter {
	if len(chapterIDs) == 0 {
		return chapters
	}
	allowed := make(map[string]struct{}, len(chapterIDs))
	for _, chapterID := range chapterIDs {
		chapterID = strings.TrimSpace(chapterID)
		if chapterID == "" {
			continue
		}
		allowed[chapterID] = struct{}{}
	}
	selected := make([]onlinesvc.Chapter, 0, len(allowed))
	for _, chapter := range chapters {
		if _, ok := allowed[chapter.ID]; ok {
			selected = append(selected, chapter)
		}
	}
	return selected
}

func chapterIDsFromSelection(chapters []onlinesvc.Chapter) []string {
	items := make([]string, 0, len(chapters))
	for _, chapter := range chapters {
		chapterID := strings.TrimSpace(chapter.ID)
		if chapterID != "" {
			items = append(items, chapterID)
		}
	}
	sort.Strings(items)
	return items
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Service) findDuplicateJob(ctx context.Context, sourceID string, mangaID string, chapterIDs []string) (onlinesvc.DownloadJobDetail, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM download_job
		WHERE source_id = ? AND external_manga_id = ? AND status <> ?
		ORDER BY created_at DESC, id DESC
		LIMIT 20
	`, sourceID, mangaID, onlinesvc.DownloadJobCanceled)
	if err != nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("query duplicate download jobs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("scan duplicate download job: %w", err)
		}

		chapterRows, err := s.db.QueryContext(ctx, `
			SELECT external_chapter_id
			FROM download_job_chapter
			WHERE job_id = ?
			ORDER BY external_chapter_id ASC
		`, jobID)
		if err != nil {
			return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("query duplicate job chapters: %w", err)
		}

		existingChapterIDs := make([]string, 0)
		for chapterRows.Next() {
			var chapterID string
			if err := chapterRows.Scan(&chapterID); err != nil {
				chapterRows.Close()
				return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("scan duplicate job chapter: %w", err)
			}
			existingChapterIDs = append(existingChapterIDs, chapterID)
		}
		chapterRows.Close()

		if !sameStrings(existingChapterIDs, chapterIDs) {
			continue
		}

		item, err := s.GetJob(ctx, jobID)
		if err != nil {
			return onlinesvc.DownloadJobDetail{}, false, err
		}
		return item, true, nil
	}

	if err := rows.Err(); err != nil {
		return onlinesvc.DownloadJobDetail{}, false, fmt.Errorf("iterate duplicate download jobs: %w", err)
	}

	return onlinesvc.DownloadJobDetail{}, false, nil
}

func (s *Service) removeJobOutput(job onlinesvc.DownloadJob, manga onlinesvc.Manga, chapters []onlinesvc.DownloadChapter) error {
	root := s.jobOutputRoot(job.SourceID, manga)
	if job.Mode == "manga" {
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("remove manga download output: %w", err)
		}
		return nil
	}

	for _, chapter := range chapters {
		if err := os.RemoveAll(filepath.Join(root, downloadChapterDirName(chapter.Order, chapter.Title))); err != nil {
			return fmt.Errorf("remove chapter download output: %w", err)
		}
	}
	_ = os.Remove(filepath.Join(root, "metadata.json"))
	return nil
}

func downloadChapterDirName(order int, title string) string {
	return fmt.Sprintf("%03d-%s", maxInt(order, 1), sanitizeForPath(title))
}

func inferFileExtension(remoteURL string, mimeType string) string {
	if mimeType != "" {
		if extensions, err := mime.ExtensionsByType(mimeType); err == nil && len(extensions) > 0 {
			return extensions[0]
		}
	}
	ext := strings.ToLower(filepath.Ext(strings.Split(remoteURL, "?")[0]))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return ext
	default:
		return ".jpg"
	}
}

func sanitizeForPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "|", "_", "?", "_", "*", "_", "\"", "_", "<", "_", ">", "_")
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func maxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

type scanner interface {
	Scan(dest ...any) error
}

type scanTarget interface {
	Scan(dest ...any) error
}

func scanDownloadJob(row scanTarget) (onlinesvc.DownloadJob, error) {
	var job onlinesvc.DownloadJob
	if err := row.Scan(
		&job.ID,
		&job.SourceID,
		&job.MangaID,
		&job.MangaTitle,
		&job.Status,
		&job.Mode,
		&job.TotalChapters,
		&job.DoneChapters,
		&job.TotalPages,
		&job.DonePages,
		&job.FailedPages,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.FinishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return onlinesvc.DownloadJob{}, fmt.Errorf("download job not found")
		}
		return onlinesvc.DownloadJob{}, fmt.Errorf("scan download job: %w", err)
	}
	return job, nil
}

func (s *Service) logWarn(message string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(message, args...)
	}
}
