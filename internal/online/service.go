package online

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mynewmangaui/internal/config"
)

type SearchOptions struct {
	Query string
	Page  int
	Limit int
}

type BrowseOptions struct {
	Mode  string
	Page  int
	Limit int
}

type Provider interface {
	Source() Source
	Browse(context.Context, BrowseOptions) ([]Manga, error)
	Search(context.Context, SearchOptions) ([]Manga, error)
	GetManga(context.Context, string) (Manga, error)
	GetChapters(context.Context, string) ([]Chapter, error)
	GetPages(context.Context, string) ([]Page, error)
	FetchImage(context.Context, string) ([]byte, string, error)
}

type Service struct {
	sources   map[string]Source
	providers map[string]Provider
	cachePath string
	cacheTTL  time.Duration
}

func NewService(cfg config.OnlineConfig, providers ...Provider) *Service {
	service := &Service{
		sources:   make(map[string]Source),
		providers: make(map[string]Provider),
		cachePath: strings.TrimSpace(cfg.CachePath),
		cacheTTL:  time.Duration(cfg.ImageProxyTTLSeconds) * time.Second,
	}

	for _, item := range cfg.Sources {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		service.sources[id] = Source{
			ID:      id,
			Name:    strings.TrimSpace(item.Name),
			BaseURL: strings.TrimSpace(item.BaseURL),
			Enabled: cfg.Enabled && item.Enabled,
			DefaultDisplay: SourceDefaultDisplay{
				Mode:        strings.TrimSpace(item.DefaultDisplay.Mode),
				Title:       strings.TrimSpace(item.DefaultDisplay.Title),
				Description: strings.TrimSpace(item.DefaultDisplay.Description),
				Limit:       item.DefaultDisplay.Limit,
			},
		}
	}

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		source := provider.Source()
		if strings.TrimSpace(source.ID) == "" {
			continue
		}
		service.sources[source.ID] = source
		service.providers[source.ID] = provider
	}

	return service
}

func (s *Service) ListSources() []Source {
	items := make([]Source, 0, len(s.sources))
	for _, source := range s.sources {
		items = append(items, source)
	}
	return items
}

func (s *Service) Search(ctx context.Context, sourceID string, options SearchOptions) ([]Manga, error) {
	provider, err := s.provider(sourceID)
	if err != nil {
		return nil, err
	}
	return provider.Search(ctx, options)
}

func (s *Service) DefaultFeed(ctx context.Context, sourceID string, page int, limit int) (DefaultFeed, error) {
	provider, err := s.provider(sourceID)
	if err != nil {
		return DefaultFeed{}, err
	}

	source, ok := s.sources[strings.TrimSpace(sourceID)]
	if !ok {
		return DefaultFeed{}, fmt.Errorf("online source %q is not available", sourceID)
	}

	mode := strings.TrimSpace(source.DefaultDisplay.Mode)
	if mode == "" {
		mode = "latest"
	}
	if limit <= 0 {
		limit = source.DefaultDisplay.Limit
	}
	if limit <= 0 {
		limit = 30
	}
	if page <= 0 {
		page = 1
	}

	items, err := provider.Browse(ctx, BrowseOptions{
		Mode:  mode,
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		return DefaultFeed{}, err
	}

	title := strings.TrimSpace(source.DefaultDisplay.Title)
	if title == "" {
		title = source.Name
	}

	return DefaultFeed{
		SourceID:    source.ID,
		Mode:        mode,
		Title:       title,
		Description: strings.TrimSpace(source.DefaultDisplay.Description),
		Page:        page,
		Limit:       limit,
		HasMore:     len(items) >= limit,
		Items:       items,
	}, nil
}

func (s *Service) GetManga(ctx context.Context, sourceID string, mangaID string) (Manga, error) {
	provider, err := s.provider(sourceID)
	if err != nil {
		return Manga{}, err
	}
	return provider.GetManga(ctx, mangaID)
}

func (s *Service) GetChapters(ctx context.Context, sourceID string, mangaID string) ([]Chapter, error) {
	provider, err := s.provider(sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetChapters(ctx, mangaID)
}

func (s *Service) GetPages(ctx context.Context, sourceID string, chapterID string) ([]Page, error) {
	provider, err := s.provider(sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetPages(ctx, chapterID)
}

func (s *Service) FetchImage(ctx context.Context, sourceID string, remoteURL string) ([]byte, string, error) {
	if payload, mime, ok, err := s.readCachedImage(sourceID, remoteURL); err != nil {
		return nil, "", err
	} else if ok {
		return payload, mime, nil
	}

	provider, err := s.provider(sourceID)
	if err != nil {
		return nil, "", err
	}
	payload, mime, err := provider.FetchImage(ctx, remoteURL)
	if err != nil {
		return nil, "", err
	}

	_ = s.writeCachedImage(sourceID, remoteURL, payload, mime)

	return payload, mime, nil
}

func (s *Service) provider(sourceID string) (Provider, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	provider, ok := s.providers[sourceID]
	if !ok {
		return nil, fmt.Errorf("online source %q is not available", sourceID)
	}
	return provider, nil
}

type cachedImageMeta struct {
	Mime      string `json:"mime"`
	RemoteURL string `json:"remoteUrl"`
}

func (s *Service) readCachedImage(sourceID string, remoteURL string) ([]byte, string, bool, error) {
	if strings.TrimSpace(s.cachePath) == "" {
		return nil, "", false, nil
	}

	cacheFile, metaFile := s.cacheFilePaths(sourceID, remoteURL)
	info, err := os.Stat(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}

	if s.cacheTTL > 0 && time.Since(info.ModTime()) > s.cacheTTL {
		return nil, "", false, nil
	}

	payload, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, "", false, err
	}

	metaPayload, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, "", false, err
	}

	var meta cachedImageMeta
	if err := json.Unmarshal(metaPayload, &meta); err != nil {
		return nil, "", false, err
	}
	if strings.TrimSpace(meta.Mime) == "" {
		meta.Mime = "application/octet-stream"
	}
	if !isCacheableImagePayload(payload, meta.Mime) {
		_ = os.Remove(cacheFile)
		_ = os.Remove(metaFile)
		return nil, "", false, nil
	}

	return payload, meta.Mime, true, nil
}

func (s *Service) writeCachedImage(sourceID string, remoteURL string, payload []byte, mime string) error {
	if strings.TrimSpace(s.cachePath) == "" {
		return nil
	}
	if !isCacheableImagePayload(payload, mime) {
		return nil
	}

	cacheFile, metaFile := s.cacheFilePaths(sourceID, remoteURL)
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(cacheFile, payload, 0o644); err != nil {
		return err
	}

	metaPayload, err := json.Marshal(cachedImageMeta{
		Mime:      mime,
		RemoteURL: remoteURL,
	})
	if err != nil {
		return err
	}

	if err := os.WriteFile(metaFile, metaPayload, 0o644); err != nil {
		return err
	}

	return nil
}

func (s *Service) cacheFilePaths(sourceID string, remoteURL string) (string, string) {
	sum := sha1.Sum([]byte(sourceID + "|" + remoteURL))
	key := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.cachePath, "images", sanitizePathSegment(sourceID))
	return filepath.Join(dir, key+".bin"), filepath.Join(dir, key+".json")
}

func isCacheableImagePayload(payload []byte, mime string) bool {
	if len(payload) == 0 {
		return false
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	return strings.HasPrefix(mime, "image/")
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "|", "_", "?", "_", "*", "_", "\"", "_", "<", "_", ">", "_")
	return replacer.Replace(value)
}
