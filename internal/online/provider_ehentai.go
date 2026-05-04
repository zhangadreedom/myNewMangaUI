package online

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"mynewmangaui/internal/config"
)

const ehentaiImageAccept = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"

var (
	reEHentaiGalleryLink  = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"']+)?/g/(\d+)/([0-9a-z]+)/?[^"']*["'][^>]*>(.*?)</a>`)
	reEHentaiGalleryURL   = regexp.MustCompile(`(?is)/g/(\d+)/([0-9a-z]+)/?`)
	reEHentaiImagePageURL = regexp.MustCompile(`(?is)<a[^>]+href=["']((?:https?://[^"']+)?/s/[0-9a-z]+/\d+-\d+[^"']*)["'][^>]*>`)
	reEHentaiPageImage    = regexp.MustCompile(`(?is)<img[^>]+id=["']img["'][^>]+src=["']([^"']+)["']`)
	reEHentaiNextImage    = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']+)["'][^>]*>\s*<img[^>]+id=["']img["']`)
	reEHentaiTitleGN      = regexp.MustCompile(`(?is)<h1[^>]+id=["']gn["'][^>]*>([^<]*)</h1>`)
	reEHentaiTitleGJ      = regexp.MustCompile(`(?is)<h1[^>]+id=["']gj["'][^>]*>([^<]*)</h1>`)
	reEHentaiGlink        = regexp.MustCompile(`(?is)<div[^>]+class=["'][^"']*\bglink\b[^"']*["'][^>]*>(.*?)</div>`)
	reEHentaiCover        = regexp.MustCompile(`(?is)<div[^>]+id=["']gd1["'][^>]*>.*?<img[^>]+(?:data-src|src)=["']([^"']+)["']`)
	reEHentaiCoverStyle   = regexp.MustCompile(`(?is)<div[^>]+id=["']gd1["'][^>]*>.*?url\(([^)]+)\)`)
	reEHentaiTag          = regexp.MustCompile(`(?is)<a[^>]+id=["']ta_[^"']+["'][^>]*>(.*?)</a>`)
	reEHentaiLength       = regexp.MustCompile(`(?is)<td[^>]*>\s*Length:\s*</td>\s*<td[^>]*>\s*(\d+)\s+pages?`)
	reEHentaiImgSrc       = regexp.MustCompile(`(?is)(?:data-src|data-original|src)=["']([^"']+)["']`)
)

type ProviderEHentai struct {
	source config.OnlineSourceConfig
	client *httpClient
}

func NewEHentaiProvider(cfg config.OnlineConfig, source config.OnlineSourceConfig) (*ProviderEHentai, error) {
	client, err := newHTTPClient(cfg, source)
	if err != nil {
		return nil, err
	}
	return &ProviderEHentai{source: source, client: client}, nil
}

func (p *ProviderEHentai) Source() Source {
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

func (p *ProviderEHentai) Browse(ctx context.Context, options BrowseOptions) ([]Manga, error) {
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	switch mode {
	case "", "latest":
		return p.search(ctx, SearchOptions{Page: options.Page, Limit: options.Limit})
	default:
		return nil, fmt.Errorf("ehentai browse mode %q is not supported", options.Mode)
	}
}

func (p *ProviderEHentai) Search(ctx context.Context, options SearchOptions) ([]Manga, error) {
	return p.search(ctx, options)
}

func (p *ProviderEHentai) GetManga(ctx context.Context, mangaID string) (Manga, error) {
	if err := p.ensureSession(); err != nil {
		return Manga{}, err
	}
	gid, token, err := splitEHentaiID(mangaID)
	if err != nil {
		return Manga{}, err
	}

	payload, _, err := p.client.get(ctx, ehentaiGalleryPath(gid, token), nil)
	if err != nil {
		return Manga{}, err
	}

	page := string(payload)
	title := extractFirstMatch(page, reEHentaiTitleGN, reEHentaiTitleGJ)
	if title == "" {
		title = "Gallery " + gid
	}
	item := Manga{
		SourceID:  p.source.ID,
		ID:        ehentaiID(gid, token),
		Title:     title,
		CoverURL:  p.parseCoverURL(page),
		Tags:      uniqueStrings(parseTextMatches(page, reEHentaiTag)),
		PageCount: parseEHentaiPageCount(page),
		SourceURL: ehentaiAbsoluteURL(p.client.baseURL, ehentaiGalleryPath(gid, token)),
	}
	item.ChapterCount = 1
	return item, nil
}

func (p *ProviderEHentai) GetChapters(ctx context.Context, mangaID string) ([]Chapter, error) {
	manga, err := p.GetManga(ctx, mangaID)
	if err != nil {
		return nil, err
	}
	return []Chapter{{
		SourceID:  p.source.ID,
		MangaID:   manga.ID,
		ID:        manga.ID,
		Title:     "Gallery",
		Order:     1,
		PageCount: manga.PageCount,
	}}, nil
}

func (p *ProviderEHentai) GetPages(ctx context.Context, chapterID string) ([]Page, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}
	gid, token, err := splitEHentaiID(chapterID)
	if err != nil {
		return nil, err
	}

	imagePages, totalPages, err := p.fetchAllImagePageURLs(ctx, gid, token)
	if err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(imagePages))
	seenImagePages := make(map[string]struct{}, len(imagePages))
	for _, imagePageURL := range imagePages {
		seenImagePages[imagePageURL] = struct{}{}
	}
	for index := 0; index < len(imagePages); index++ {
		imagePageURL := imagePages[index]
		remoteURL, nextImagePageURL, err := p.fetchOriginalImagePage(ctx, imagePageURL)
		if err != nil {
			return nil, fmt.Errorf("fetch ehentai page %d: %w", index+1, err)
		}
		pages = append(pages, Page{
			SourceID:  p.source.ID,
			ChapterID: ehentaiID(gid, token),
			ID:        ehentaiID(gid, token) + "-" + strconv.Itoa(index+1),
			Index:     index,
			RemoteURL: remoteURL,
		})
		if totalPages > 0 && len(pages) >= totalPages {
			break
		}
		if nextImagePageURL != "" && (totalPages == 0 || len(imagePages) < totalPages) {
			if _, ok := seenImagePages[nextImagePageURL]; !ok {
				seenImagePages[nextImagePageURL] = struct{}{}
				imagePages = append(imagePages, nextImagePageURL)
			}
		}
	}
	return pages, nil
}

func (p *ProviderEHentai) FetchImage(ctx context.Context, remoteURL string) ([]byte, string, error) {
	if err := p.ensureSession(); err != nil {
		return nil, "", err
	}
	return p.client.getAbsolute(ctx, remoteURL, ehentaiImageAccept)
}

func (p *ProviderEHentai) parseCoverURL(page string) string {
	if coverURL := ehentaiFirstImageURL(p.client.baseURL, page, reEHentaiCoverStyle); coverURL != "" {
		return coverURL
	}
	return ehentaiFirstImageURL(p.client.baseURL, page, reEHentaiCover)
}

func (p *ProviderEHentai) ensureSession() error {
	if p == nil || p.client == nil {
		return fmt.Errorf("ehentai provider is not initialized")
	}
	if cookieHeader := strings.TrimSpace(p.source.CookieHeader); cookieHeader != "" {
		cookies := parseCookieHeader(cookieHeader)
		for _, target := range p.sessionTargets() {
			p.client.client.Jar.SetCookies(target, cookies)
		}
	}
	return nil
}

func (p *ProviderEHentai) sessionTargets() []*url.URL {
	targets := make([]*url.URL, 0, 2)
	for _, raw := range []string{p.source.BaseURL, "https://e-hentai.org", "https://exhentai.org"} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err == nil && parsed != nil && parsed.Host != "" {
			targets = append(targets, parsed)
		}
	}
	return targets
}

func (p *ProviderEHentai) search(ctx context.Context, options SearchOptions) ([]Manga, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}

	query := url.Values{}
	if searchText := strings.TrimSpace(options.Query); searchText != "" {
		query.Set("f_search", searchText)
	}
	if options.Page > 1 {
		query.Set("page", strconv.Itoa(options.Page-1))
	}

	payload, _, err := p.client.get(ctx, "/", query)
	if err != nil {
		return nil, err
	}

	items := parseEHentaiGalleryList(p.source.ID, p.client.baseURL, string(payload))
	if options.Limit > 0 && len(items) > options.Limit {
		items = items[:options.Limit]
	}
	return items, nil
}

func (p *ProviderEHentai) fetchAllImagePageURLs(ctx context.Context, gid string, token string) ([]string, int, error) {
	firstPage, _, err := p.client.get(ctx, ehentaiGalleryPath(gid, token), nil)
	if err != nil {
		return nil, 0, err
	}

	totalPages := parseEHentaiPageCount(string(firstPage))
	galleryPages := 1
	if totalPages > 0 {
		galleryPages = (totalPages + 39) / 40
	}
	if galleryPages < 1 {
		galleryPages = 1
	}

	seen := make(map[string]struct{})
	items := make([]string, 0, totalPages)
	appendLinks := func(raw string) {
		for _, link := range parseEHentaiImagePageLinks(p.client.baseURL, raw) {
			if _, ok := seen[link]; ok {
				continue
			}
			seen[link] = struct{}{}
			items = append(items, link)
		}
	}
	appendLinks(string(firstPage))

	for page := 1; page < galleryPages; page++ {
		query := url.Values{}
		query.Set("p", strconv.Itoa(page))
		payload, _, err := p.client.get(ctx, ehentaiGalleryPath(gid, token), query)
		if err != nil {
			return nil, totalPages, err
		}
		appendLinks(string(payload))
	}
	if len(items) == 0 {
		return nil, totalPages, fmt.Errorf("unable to parse ehentai gallery pages")
	}
	return items, totalPages, nil
}

func (p *ProviderEHentai) fetchOriginalImagePage(ctx context.Context, imagePageURL string) (string, string, error) {
	payload, _, err := p.client.getAbsolute(ctx, imagePageURL, "text/html,application/xhtml+xml,*/*;q=0.8")
	if err != nil {
		return "", "", err
	}
	page := string(payload)
	imageURL := extractFirstMatch(page, reEHentaiPageImage)
	if imageURL == "" {
		return "", "", fmt.Errorf("unable to parse image url")
	}
	nextImagePageURL := extractFirstMatch(page, reEHentaiNextImage)
	return ehentaiAbsoluteURL(p.client.baseURL, imageURL), ehentaiAbsoluteURL(p.client.baseURL, nextImagePageURL), nil
}

func parseEHentaiGalleryList(sourceID string, base *url.URL, page string) []Manga {
	matches := reEHentaiGalleryLink.FindAllStringSubmatchIndex(page, -1)
	items := make([]Manga, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for _, match := range matches {
		if len(match) < 8 {
			continue
		}
		gid := strings.TrimSpace(page[match[2]:match[3]])
		token := strings.TrimSpace(page[match[4]:match[5]])
		id := ehentaiID(gid, token)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		windowStart := maxEHentaiInt(0, match[0]-1800)
		windowEnd := minEHentaiInt(len(page), match[1]+2800)
		fragment := page[windowStart:windowEnd]
		title := extractFirstMatch(fragment, reEHentaiGlink)
		if title == "" {
			title = sanitizeText(page[match[6]:match[7]])
		}
		if title == "" {
			title = extractFirstMatch(fragment, reTitleAttr, reAltAttr)
		}
		if title == "" {
			title = "Gallery " + gid
		}

		items = append(items, Manga{
			SourceID:  sourceID,
			ID:        id,
			Title:     title,
			CoverURL:  ehentaiFirstImageURL(base, fragment, reEHentaiImgSrc),
			SourceURL: ehentaiAbsoluteURL(base, ehentaiGalleryPath(gid, token)),
		})
	}

	return items
}

func parseEHentaiImagePageLinks(base *url.URL, page string) []string {
	matches := reEHentaiImagePageURL.FindAllStringSubmatch(page, -1)
	items := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		link := ehentaiAbsoluteURL(base, html.UnescapeString(match[1]))
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		items = append(items, link)
	}
	return items
}

func parseEHentaiPageCount(page string) int {
	match := reEHentaiLength.FindStringSubmatch(page)
	if len(match) < 2 {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(match[1]))
	return count
}

func ehentaiFirstImageURL(base *url.URL, text string, expression *regexp.Regexp) string {
	matches := expression.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(html.UnescapeString(match[1]))
		value = strings.Trim(value, `"' `)
		if !isUsableEHentaiImageURL(value) {
			continue
		}
		return ehentaiAbsoluteURL(base, value)
	}
	return ""
}

func isUsableEHentaiImageURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") {
		return false
	}
	if strings.Contains(lower, "/g/ygm.png") || strings.Contains(lower, "/g/mr.gif") {
		return false
	}
	return true
}

func splitEHentaiID(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", fmt.Errorf("manga id is required")
	}
	if decoded, err := url.PathUnescape(value); err == nil {
		value = strings.TrimSpace(decoded)
	}
	if match := reEHentaiGalleryURL.FindStringSubmatch(value); len(match) >= 3 {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2]), nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		parts = strings.Split(value, "-")
	}
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid ehentai gallery id")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func ehentaiID(gid string, token string) string {
	gid = strings.TrimSpace(gid)
	token = strings.Trim(strings.TrimSpace(token), "/")
	if gid == "" || token == "" {
		return ""
	}
	return gid + ":" + token
}

func ehentaiGalleryPath(gid string, token string) string {
	return "/g/" + strings.TrimSpace(gid) + "/" + strings.Trim(strings.TrimSpace(token), "/") + "/"
}

func ehentaiAbsoluteURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	if base != nil {
		return base.ResolveReference(parsed).String()
	}
	return raw
}

func minEHentaiInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxEHentaiInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

var _ Provider = (*ProviderEHentai)(nil)
