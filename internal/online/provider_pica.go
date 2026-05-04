package online

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mynewmangaui/internal/config"
)

const (
	picaAPIKey             = "C69BAF41DA5ABD1FFEDC6D2FEA56B"
	picaAccept             = "application/vnd.picacomic.com.v1+json"
	picaAppChannel         = "3"
	picaAppVersion         = "2.2.1.3.3.4"
	picaAppBuildVersion    = "45"
	picaPlatform           = "android"
	picaImageQuality       = "original"
	picaUpdateVersion      = "v1.5.4"
	picaDefaultUserAgent   = "okhttp/3.8.1"
	picaSignatureSecret    = "~d}$Q7$eIni=V)9\\\\RK/P.RM4;9[7|@/CA}b~OW!3?EV`:<>M7pddUBL5n|0/*Cn"
	picaDefaultImageAccept = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"
)

type ProviderPica struct {
	source config.OnlineSourceConfig
	client *httpClient

	mu    sync.Mutex
	token string
}

type picaEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

type picaLoginData struct {
	Token string `json:"token"`
}

type picaComicsData struct {
	Comics picaComicList `json:"comics"`
}

type picaComicData struct {
	Comic picaComic `json:"comic"`
}

type picaEpsData struct {
	Eps picaEpisodeList `json:"eps"`
}

type picaPagesData struct {
	Pages picaPageList `json:"pages"`
}

type picaComicList struct {
	Docs  []picaComic `json:"docs"`
	Page  int         `json:"page"`
	Pages int         `json:"pages"`
	Total int         `json:"total"`
}

type picaEpisodeList struct {
	Docs  []picaEpisode `json:"docs"`
	Page  int           `json:"page"`
	Pages int           `json:"pages"`
	Total int           `json:"total"`
}

type picaPageList struct {
	Docs  []picaPageImage `json:"docs"`
	Page  int             `json:"page"`
	Pages int             `json:"pages"`
	Total int             `json:"total"`
}

type picaComic struct {
	ID          string      `json:"_id"`
	Title       string      `json:"title"`
	Author      string      `json:"author"`
	Categories  []string    `json:"categories"`
	Tags        []string    `json:"tags"`
	PagesCount  int         `json:"pagesCount"`
	EpsCount    int         `json:"epsCount"`
	TotalViews  int         `json:"totalViews"`
	TotalLikes  int         `json:"totalLikes"`
	Finished    bool        `json:"finished"`
	Thumb       picaMedia   `json:"thumb"`
	Description string      `json:"description"`
	UpdatedAt   string      `json:"updated_at"`
	CreatedAt   string      `json:"created_at"`
	Creator     picaCreator `json:"_creator"`
}

type picaCreator struct {
	Name string `json:"name"`
}

type picaEpisode struct {
	ID    string `json:"_id"`
	Title string `json:"title"`
	Order int    `json:"order"`
}

type picaPageImage struct {
	ID    string    `json:"_id"`
	Media picaMedia `json:"media"`
}

type picaMedia struct {
	FileServer string `json:"fileServer"`
	Path       string `json:"path"`
}

func NewPicaProvider(cfg config.OnlineConfig, source config.OnlineSourceConfig) (*ProviderPica, error) {
	if strings.TrimSpace(source.UserAgent) == "" {
		source.UserAgent = picaDefaultUserAgent
	}
	client, err := newHTTPClient(cfg, source)
	if err != nil {
		return nil, err
	}
	return &ProviderPica{source: source, client: client, token: strings.TrimSpace(source.SessionCookie)}, nil
}

func (p *ProviderPica) Source() Source {
	return Source{
		ID:      p.source.ID,
		Name:    p.source.Name,
		BaseURL: p.source.BaseURL,
		Enabled: p.source.Enabled,
		DefaultDisplay: SourceDefaultDisplay{
			Mode:        strings.TrimSpace(p.source.DefaultDisplay.Mode),
			Title:       strings.TrimSpace(p.source.DefaultDisplay.Title),
			Description: strings.TrimSpace(p.source.DefaultDisplay.Description),
			Limit:       p.source.DefaultDisplay.Limit,
		},
	}
}

func (p *ProviderPica) Browse(ctx context.Context, options BrowseOptions) ([]Manga, error) {
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	switch mode {
	case "", "latest":
		query := url.Values{}
		query.Set("page", strconv.Itoa(normalizePicaPage(options.Page)))
		query.Set("s", "dd")
		var payload picaComicsData
		if err := p.apiGet(ctx, "comics", query, &payload); err != nil {
			return nil, err
		}
		return p.mangaList(payload.Comics.Docs, options.Limit), nil
	default:
		return nil, fmt.Errorf("pica browse mode %q is not supported", options.Mode)
	}
}

func (p *ProviderPica) Search(ctx context.Context, options SearchOptions) ([]Manga, error) {
	queryText := strings.TrimSpace(options.Query)
	if queryText == "" {
		return p.Browse(ctx, BrowseOptions{Mode: "latest", Page: options.Page, Limit: options.Limit})
	}

	body := map[string]any{
		"categories": []string{},
		"keyword":    queryText,
		"sort":       "dd",
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(normalizePicaPage(options.Page)))
	var payload picaComicsData
	if err := p.apiPostJSON(ctx, "comics/advanced-search", query, body, &payload); err != nil {
		return nil, err
	}
	return p.mangaList(payload.Comics.Docs, options.Limit), nil
}

func (p *ProviderPica) GetManga(ctx context.Context, mangaID string) (Manga, error) {
	mangaID = strings.TrimSpace(mangaID)
	if mangaID == "" {
		return Manga{}, fmt.Errorf("manga id is required")
	}

	var payload picaComicData
	if err := p.apiGet(ctx, "comics/"+url.PathEscape(mangaID), nil, &payload); err != nil {
		return Manga{}, err
	}
	item := p.manga(payload.Comic)
	if item.ID == "" {
		item.ID = mangaID
	}
	if item.Title == "" {
		item.Title = "Pica " + mangaID
	}
	return item, nil
}

func (p *ProviderPica) GetChapters(ctx context.Context, mangaID string) ([]Chapter, error) {
	mangaID = strings.TrimSpace(mangaID)
	if mangaID == "" {
		return nil, fmt.Errorf("manga id is required")
	}

	episodes, err := p.fetchAllEpisodes(ctx, mangaID)
	if err != nil {
		return nil, err
	}
	items := make([]Chapter, 0, len(episodes))
	for index, episode := range episodes {
		order := episode.Order
		if order <= 0 {
			order = index + 1
		}
		title := strings.TrimSpace(episode.Title)
		if title == "" {
			title = "第 " + strconv.Itoa(order) + " 话"
		}
		items = append(items, Chapter{
			SourceID: p.source.ID,
			MangaID:  mangaID,
			ID:       picaChapterID(mangaID, order),
			Title:    title,
			Order:    order,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Order == items[j].Order {
			return items[i].ID < items[j].ID
		}
		return items[i].Order < items[j].Order
	})
	return items, nil
}

func (p *ProviderPica) GetPages(ctx context.Context, chapterID string) ([]Page, error) {
	mangaID, order, err := splitPicaChapterID(chapterID)
	if err != nil {
		return nil, err
	}

	images, err := p.fetchAllPages(ctx, mangaID, order)
	if err != nil {
		return nil, err
	}

	items := make([]Page, 0, len(images))
	for index, image := range images {
		remoteURL := picaMediaURL(image.Media)
		if remoteURL == "" {
			continue
		}
		pageID := strings.TrimSpace(image.ID)
		if pageID == "" {
			pageID = chapterID + "-" + strconv.Itoa(index+1)
		}
		items = append(items, Page{
			SourceID:  p.source.ID,
			ChapterID: chapterID,
			ID:        pageID,
			Index:     index,
			RemoteURL: remoteURL,
		})
	}
	return items, nil
}

func (p *ProviderPica) FetchImage(ctx context.Context, remoteURL string) ([]byte, string, error) {
	if p == nil || p.client == nil {
		return nil, "", fmt.Errorf("pica provider is not initialized")
	}
	targetURL, err := url.Parse(strings.TrimSpace(remoteURL))
	if err != nil {
		return nil, "", err
	}
	if !targetURL.IsAbs() && p.client.baseURL != nil {
		targetURL = p.client.baseURL.ResolveReference(targetURL)
	}
	if err := p.client.waitForRateLimit(ctx); err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return nil, "", err
	}
	p.applyHeaders(req, "Download", *targetURL, false)
	req.Header.Set("Accept", picaDefaultImageAccept)
	if p.client.baseURL != nil {
		req.Header.Set("Referer", p.client.baseURL.String())
	}

	resp, err := p.client.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	p.client.lastAccess = time.Now()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("pica image request failed: %s (%s)", resp.Status, strings.TrimSpace(string(payload)))
	}
	mime := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mime == "" {
		mime = "application/octet-stream"
	}
	if !strings.HasPrefix(strings.ToLower(mime), "image/") {
		return nil, "", fmt.Errorf("pica image request returned non-image payload: %s (%d bytes)", mime, len(payload))
	}
	return payload, mime, nil
}

func (p *ProviderPica) fetchAllEpisodes(ctx context.Context, mangaID string) ([]picaEpisode, error) {
	var all []picaEpisode
	for page := 1; page < 1000; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		var payload picaEpsData
		if err := p.apiGet(ctx, "comics/"+url.PathEscape(mangaID)+"/eps", query, &payload); err != nil {
			return nil, err
		}
		all = append(all, payload.Eps.Docs...)
		if payload.Eps.Pages <= 0 || page >= payload.Eps.Pages || len(payload.Eps.Docs) == 0 {
			break
		}
	}
	return all, nil
}

func (p *ProviderPica) fetchAllPages(ctx context.Context, mangaID string, order int) ([]picaPageImage, error) {
	var all []picaPageImage
	for page := 1; page < 1000; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		var payload picaPagesData
		path := "comics/" + url.PathEscape(mangaID) + "/order/" + strconv.Itoa(order) + "/pages"
		if err := p.apiGet(ctx, path, query, &payload); err != nil {
			return nil, err
		}
		all = append(all, payload.Pages.Docs...)
		if payload.Pages.Pages <= 0 || page >= payload.Pages.Pages || len(payload.Pages.Docs) == 0 {
			break
		}
	}
	return all, nil
}

func (p *ProviderPica) mangaList(items []picaComic, limit int) []Manga {
	result := make([]Manga, 0, len(items))
	for _, item := range items {
		manga := p.manga(item)
		if manga.ID == "" {
			continue
		}
		result = append(result, manga)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func (p *ProviderPica) manga(item picaComic) Manga {
	id := strings.TrimSpace(item.ID)
	author := strings.TrimSpace(item.Author)
	if author == "" {
		author = strings.TrimSpace(item.Creator.Name)
	}
	return Manga{
		SourceID:     p.source.ID,
		ID:           id,
		Title:        strings.TrimSpace(item.Title),
		CoverURL:     picaMediaURL(item.Thumb),
		SourceURL:    strings.TrimRight(p.source.BaseURL, "/") + "/comics/" + id,
		Author:       author,
		Tags:         uniqueStrings(append(append([]string{}, item.Categories...), item.Tags...)),
		ChapterCount: item.EpsCount,
		PageCount:    item.PagesCount,
	}
}

func (p *ProviderPica) apiGet(ctx context.Context, path string, query url.Values, target any) error {
	return p.apiRequest(ctx, http.MethodGet, path, query, nil, target, true)
}

func (p *ProviderPica) apiPostJSON(ctx context.Context, path string, query url.Values, body any, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return p.apiRequest(ctx, http.MethodPost, path, query, payload, target, true)
}

func (p *ProviderPica) apiRequest(ctx context.Context, method string, path string, query url.Values, body []byte, target any, auth bool) error {
	if p == nil || p.client == nil || p.client.client == nil || p.client.baseURL == nil {
		return fmt.Errorf("pica provider is not initialized")
	}
	if auth {
		if err := p.ensureToken(ctx); err != nil {
			return err
		}
	}
	if err := p.client.waitForRateLimit(ctx); err != nil {
		return err
	}

	endpoint := *p.client.baseURL
	endpoint.Path = joinURLPath(p.client.baseURL.Path, path)
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return err
	}
	p.applyHeaders(req, method, endpoint, auth)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}

	resp, err := p.client.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	p.client.lastAccess = time.Now()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pica api request failed: %s (%s)", resp.Status, strings.TrimSpace(string(payload)))
	}

	var envelope picaEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("parse pica api response: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Error)
		}
		if message == "" {
			message = "code " + strconv.Itoa(envelope.Code)
		}
		return fmt.Errorf("pica api error: %s", message)
	}
	if target != nil {
		if len(bytes.TrimSpace(envelope.Data)) == 0 {
			return fmt.Errorf("pica api returned empty data")
		}
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			return fmt.Errorf("decode pica api data: %w", err)
		}
	}
	return nil
}

func (p *ProviderPica) ensureToken(ctx context.Context) error {
	p.mu.Lock()
	if strings.TrimSpace(p.token) != "" {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	username := strings.TrimSpace(p.source.Username)
	password := strings.TrimSpace(p.source.Password)
	if username == "" || password == "" {
		return fmt.Errorf("pica login requires online.sources[].username/password or sessionCookie token")
	}

	body, err := json.Marshal(map[string]string{"email": username, "password": password})
	if err != nil {
		return err
	}
	var payload picaLoginData
	if err := p.apiRequest(ctx, http.MethodPost, "auth/sign-in", nil, body, &payload, false); err != nil {
		return fmt.Errorf("pica login failed: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return fmt.Errorf("pica login returned empty token")
	}

	p.mu.Lock()
	p.token = strings.TrimSpace(payload.Token)
	p.mu.Unlock()
	return nil
}

func (p *ProviderPica) applyHeaders(req *http.Request, method string, endpoint url.URL, auth bool) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomPicaNonce()
	requestPath := strings.TrimPrefix(endpoint.RequestURI(), "/")
	signatureSource := requestPath + now + nonce + method + picaAPIKey
	signature := picaSignature(signatureSource)

	req.Header.Set("api-key", picaAPIKey)
	req.Header.Set("accept", picaAccept)
	req.Header.Set("app-channel", picaAppChannel)
	req.Header.Set("time", now)
	req.Header.Set("app-uuid", "defaultUuid")
	req.Header.Set("nonce", nonce)
	req.Header.Set("signature", signature)
	req.Header.Set("app-version", picaAppVersion)
	req.Header.Set("image-quality", picaImageQuality)
	req.Header.Set("app-platform", picaPlatform)
	req.Header.Set("app-build-version", picaAppBuildVersion)
	req.Header.Set("user-agent", firstNonEmpty(p.source.UserAgent, picaDefaultUserAgent))
	req.Header.Set("version", picaUpdateVersion)
	if auth {
		p.mu.Lock()
		token := strings.TrimSpace(p.token)
		p.mu.Unlock()
		if token != "" {
			req.Header.Set("authorization", token)
		}
	}
}

func picaSignature(source string) string {
	mac := hmac.New(sha256.New, []byte(picaSignatureSecret))
	mac.Write([]byte(strings.ToLower(source)))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomPicaNonce() string {
	var payload [16]byte
	if _, err := rand.Read(payload[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(payload[:])
}

func picaMediaURL(media picaMedia) string {
	path := strings.TrimSpace(media.Path)
	server := strings.TrimRight(strings.TrimSpace(media.FileServer), "/")
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if server == "" {
		return path
	}
	if strings.HasSuffix(server, "/static") {
		return server + "/" + strings.TrimLeft(path, "/")
	}
	return server + "/static/" + strings.TrimLeft(path, "/")
}

func picaChapterID(mangaID string, order int) string {
	return strings.TrimSpace(mangaID) + ":" + strconv.Itoa(order)
}

func splitPicaChapterID(chapterID string) (string, int, error) {
	chapterID = strings.TrimSpace(chapterID)
	parts := strings.Split(chapterID, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid pica chapter id")
	}
	order, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || order <= 0 {
		return "", 0, fmt.Errorf("invalid pica chapter order")
	}
	mangaID := strings.TrimSpace(parts[0])
	if mangaID == "" {
		return "", 0, fmt.Errorf("manga id is required")
	}
	return mangaID, order, nil
}

func normalizePicaPage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var _ Provider = (*ProviderPica)(nil)
