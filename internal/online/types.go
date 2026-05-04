package online

type Source struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	BaseURL        string               `json:"baseUrl"`
	Enabled        bool                 `json:"enabled"`
	DefaultDisplay SourceDefaultDisplay `json:"defaultDisplay,omitempty"`
	CreatedAt      string               `json:"createdAt,omitempty"`
	UpdatedAt      string               `json:"updatedAt,omitempty"`
}

type SourceDefaultDisplay struct {
	Mode        string `json:"mode,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type Manga struct {
	SourceID        string   `json:"sourceId"`
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	CoverURL        string   `json:"coverUrl,omitempty"`
	SourceURL       string   `json:"sourceUrl,omitempty"`
	Author          string   `json:"author,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	ChapterCount    int      `json:"chapterCount,omitempty"`
	PageCount       int      `json:"pageCount,omitempty"`
	CacheStatus     string   `json:"cacheStatus,omitempty"`
	LastSeenAt      string   `json:"lastSeenAt,omitempty"`
	LastFetchedAt   string   `json:"lastFetchedAt,omitempty"`
	DetailCheckedAt string   `json:"detailCheckedAt,omitempty"`
	Favorite        bool     `json:"favorite,omitempty"`
	Following       bool     `json:"following,omitempty"`
	HasUpdate       bool     `json:"hasUpdate,omitempty"`
	LatestChapterID string   `json:"latestChapterId,omitempty"`
}

type Chapter struct {
	SourceID   string `json:"sourceId"`
	MangaID    string `json:"mangaId"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Order      int    `json:"order"`
	PageCount  int    `json:"pageCount"`
	LastSeenAt string `json:"lastSeenAt,omitempty"`
}

type Page struct {
	SourceID  string `json:"sourceId"`
	ChapterID string `json:"chapterId"`
	ID        string `json:"id"`
	Index     int    `json:"index"`
	RemoteURL string `json:"remoteUrl,omitempty"`
	ImageURL  string `json:"imageUrl,omitempty"`
}

type DefaultFeed struct {
	SourceID    string  `json:"sourceId"`
	Mode        string  `json:"mode"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Page        int     `json:"page"`
	Limit       int     `json:"limit"`
	HasMore     bool    `json:"hasMore"`
	Items       []Manga `json:"items"`
}

type DownloadJobStatus string

const (
	DownloadJobQueued     DownloadJobStatus = "queued"
	DownloadJobRunning    DownloadJobStatus = "running"
	DownloadJobProcessing DownloadJobStatus = "processing"
	DownloadJobPaused     DownloadJobStatus = "paused"
	DownloadJobDone       DownloadJobStatus = "done"
	DownloadJobFailed     DownloadJobStatus = "failed"
	DownloadJobCanceled   DownloadJobStatus = "canceled"
)

type DownloadJob struct {
	ID            string            `json:"id"`
	SourceID      string            `json:"sourceId"`
	MangaID       string            `json:"mangaId"`
	MangaTitle    string            `json:"mangaTitle"`
	CoverURL      string            `json:"coverUrl,omitempty"`
	Status        DownloadJobStatus `json:"status"`
	Mode          string            `json:"mode"`
	TotalChapters int               `json:"totalChapters"`
	DoneChapters  int               `json:"doneChapters"`
	TotalPages    int               `json:"totalPages"`
	DonePages     int               `json:"donePages"`
	FailedPages   int               `json:"failedPages"`
	ErrorMessage  string            `json:"errorMessage,omitempty"`
	CreatedAt     string            `json:"createdAt,omitempty"`
	UpdatedAt     string            `json:"updatedAt,omitempty"`
	StartedAt     string            `json:"startedAt,omitempty"`
	FinishedAt    string            `json:"finishedAt,omitempty"`
}

type DownloadChapter struct {
	JobID       string `json:"jobId"`
	SourceID    string `json:"sourceId"`
	MangaID     string `json:"mangaId"`
	ChapterID   string `json:"chapterId"`
	Title       string `json:"title"`
	Order       int    `json:"order"`
	PageCount   int    `json:"pageCount"`
	DonePages   int    `json:"donePages"`
	FailedPages int    `json:"failedPages"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type DownloadJobDetail struct {
	DownloadJob
	Chapters []DownloadChapter `json:"chapters,omitempty"`
	Existing bool              `json:"existing,omitempty"`
}

type DownloadItemStatus string

const (
	DownloadItemQueued  DownloadItemStatus = "queued"
	DownloadItemRunning DownloadItemStatus = "running"
	DownloadItemDone    DownloadItemStatus = "done"
	DownloadItemFailed  DownloadItemStatus = "failed"
	DownloadItemSkipped DownloadItemStatus = "skipped"
)

type DownloadItem struct {
	ID         string             `json:"id"`
	JobID      string             `json:"jobId"`
	SourceID   string             `json:"sourceId"`
	ChapterID  string             `json:"chapterId"`
	PageIndex  int                `json:"pageIndex"`
	RemoteURL  string             `json:"remoteUrl,omitempty"`
	LocalPath  string             `json:"localPath,omitempty"`
	Status     DownloadItemStatus `json:"status"`
	RetryCount int                `json:"retryCount"`
	Error      string             `json:"error,omitempty"`
}
