package online

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"mynewmangaui/internal/config"
)

var (
	reAlbumLink   = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"']+)?/album/(\d+)[^"']*["'][^>]*>(.*?)</a>`)
	rePhotoLink   = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"']+)?/photo/(\d+)[^"']*["'][^>]*>(.*?)</a>`)
	reImgSrc      = regexp.MustCompile(`(?is)(?:data-original|data-src|src)=["']([^"']+)["']`)
	reTitleAttr   = regexp.MustCompile(`(?is)title=["']([^"']+)["']`)
	reAltAttr     = regexp.MustCompile(`(?is)alt=["']([^"']+)["']`)
	reMetaTitle   = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	reMetaImage   = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`)
	reHTMLTitle   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reTagLink     = regexp.MustCompile(`(?is)<a[^>]+href=["'][^"']*/tag/[^"']*["'][^>]*>(.*?)</a>`)
	reAuthorLabel = regexp.MustCompile(`(?is)(?:作者|author)[^<]*</[^>]+>\s*<[^>]+>(.*?)</`)
)

type Provider18Comic struct {
	source config.OnlineSourceConfig
	client *httpClient
}

func New18ComicProvider(cfg config.OnlineConfig, source config.OnlineSourceConfig) (*Provider18Comic, error) {
	client, err := newHTTPClient(cfg, source)
	if err != nil {
		return nil, err
	}

	return &Provider18Comic{
		source: source,
		client: client,
	}, nil
}

func (p *Provider18Comic) Source() Source {
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

func (p *Provider18Comic) Browse(ctx context.Context, options BrowseOptions) ([]Manga, error) {
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	switch mode {
	case "", "latest":
		items, err := p.searchMobile(ctx, SearchOptions{
			Query: "",
			Page:  options.Page,
			Limit: options.Limit,
		})
		if err != nil {
			return nil, err
		}
		if options.Limit > 0 && len(items) > options.Limit {
			items = items[:options.Limit]
		}
		return items, nil
	default:
		return nil, fmt.Errorf("18comic browse mode %q is not supported", options.Mode)
	}
}

func (p *Provider18Comic) Search(ctx context.Context, options SearchOptions) ([]Manga, error) {
	items, err := p.searchMobile(ctx, options)
	if err != nil {
		return nil, err
	}
	if options.Limit > 0 && len(items) > options.Limit {
		items = items[:options.Limit]
	}
	return items, nil
}

func (p *Provider18Comic) GetManga(ctx context.Context, mangaID string) (Manga, error) {
	return p.getMangaMobile(ctx, mangaID)
}

func (p *Provider18Comic) GetChapters(ctx context.Context, mangaID string) ([]Chapter, error) {
	return p.getChaptersMobile(ctx, mangaID)
}

func (p *Provider18Comic) GetPages(ctx context.Context, chapterID string) ([]Page, error) {
	return p.getPagesMobile(ctx, chapterID)
}

func (p *Provider18Comic) FetchImage(ctx context.Context, remoteURL string) ([]byte, string, error) {
	return p.fetchMobileImage(ctx, remoteURL)
}

func (p *Provider18Comic) ensureSession(ctx context.Context) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("18comic provider is not initialized")
	}

	targets := p.sessionTargets()

	if cookieHeader := strings.TrimSpace(p.source.CookieHeader); cookieHeader != "" {
		cookies := parseCookieHeader(cookieHeader)
		for _, target := range targets {
			p.client.client.Jar.SetCookies(target, cookies)
		}
	}

	if cookieValue := strings.TrimSpace(p.source.SessionCookie); cookieValue != "" {
		cookies := httpCookieList{
			{Name: "AVS", Value: cookieValue},
		}.toHTTPCookies()
		for _, target := range targets {
			p.client.client.Jar.SetCookies(target, cookies)
		}
		return nil
	}

	if strings.TrimSpace(p.source.Username) == "" || strings.TrimSpace(p.source.Password) == "" {
		return nil
	}

	form := url.Values{}
	form.Set("username", p.source.Username)
	form.Set("password", p.source.Password)

	body, _, err := p.client.postForm(ctx, "/login", form)
	if err != nil {
		return fmt.Errorf("18comic login failed: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// Some deployments return non-JSON login responses. Keep the jar state if
		// upstream already set cookies on redirect or response headers.
		return nil
	}

	sessionValue, _ := payload["s"].(string)
	if strings.TrimSpace(sessionValue) == "" {
		return nil
	}

	cookies := httpCookieList{
		{Name: "AVS", Value: sessionValue},
	}.toHTTPCookies()
	for _, target := range targets {
		p.client.client.Jar.SetCookies(target, cookies)
	}

	return nil
}

func (p *Provider18Comic) sessionTargets() []*url.URL {
	targets := make([]*url.URL, 0, 1+len(default18ComicAPIBaseURLs))
	seen := make(map[string]struct{}, 1+len(default18ComicAPIBaseURLs))

	appendTarget := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed == nil || parsed.Host == "" {
			return
		}
		key := parsed.Scheme + "://" + parsed.Host
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, parsed)
	}

	appendTarget(p.source.BaseURL)
	for _, raw := range default18ComicAPIBaseURLs {
		appendTarget(raw)
	}

	return targets
}

func parseCookieHeader(raw string) []*http.Cookie {
	parts := strings.Split(raw, ";")
	items := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		chunks := strings.SplitN(part, "=", 2)
		if len(chunks) != 2 {
			continue
		}
		name := strings.TrimSpace(chunks[0])
		value := strings.TrimSpace(chunks[1])
		if name == "" {
			continue
		}
		items = append(items, &http.Cookie{
			Name:  name,
			Value: value,
			Path:  "/",
		})
	}
	return items
}

func parseSearchPageHTML(sourceID string, page string) []Manga {
	matches := reAlbumLink.FindAllStringSubmatch(page, -1)
	items := make([]Manga, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		albumID := strings.TrimSpace(match[1])
		if albumID == "" {
			continue
		}
		if _, ok := seen[albumID]; ok {
			continue
		}
		seen[albumID] = struct{}{}

		fragment := match[2]
		title := extractFirstMatch(fragment, reTitleAttr, reAltAttr)
		if title == "" {
			title = sanitizeText(fragment)
		}
		if title == "" {
			title = "Album " + albumID
		}

		items = append(items, Manga{
			SourceID: sourceID,
			ID:       albumID,
			Title:    title,
			CoverURL: extractFirstMatch(fragment, reImgSrc),
		})
	}

	return items
}

func parseAlbumHTML(sourceID string, mangaID string, page string) (Manga, error) {
	title := extractFirstMatch(page, reMetaTitle, reHTMLTitle)
	if title == "" {
		return Manga{}, fmt.Errorf("unable to parse album title")
	}

	return Manga{
		SourceID: sourceID,
		ID:       strings.TrimSpace(mangaID),
		Title:    sanitizeText(title),
		CoverURL: extractFirstMatch(page, reMetaImage),
		Author:   sanitizeText(extractFirstMatch(page, reAuthorLabel)),
		Tags:     uniqueStrings(parseTextMatches(page, reTagLink)),
	}, nil
}

func parseAlbumChaptersHTML(sourceID string, mangaID string, page string) []Chapter {
	matches := rePhotoLink.FindAllStringSubmatch(page, -1)
	items := make([]Chapter, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for index, match := range matches {
		if len(match) < 3 {
			continue
		}

		chapterID := strings.TrimSpace(match[1])
		if chapterID == "" {
			continue
		}
		if _, ok := seen[chapterID]; ok {
			continue
		}
		seen[chapterID] = struct{}{}

		title := extractFirstMatch(match[2], reTitleAttr)
		if title == "" {
			title = sanitizeText(match[2])
		}
		if title == "" {
			title = "Chapter " + strconv.Itoa(index+1)
		}

		items = append(items, Chapter{
			SourceID:  sourceID,
			MangaID:   strings.TrimSpace(mangaID),
			ID:        chapterID,
			Title:     title,
			Order:     index + 1,
			PageCount: 0,
		})
	}

	if len(items) == 0 && strings.TrimSpace(mangaID) != "" {
		items = append(items, Chapter{
			SourceID:  sourceID,
			MangaID:   strings.TrimSpace(mangaID),
			ID:        strings.TrimSpace(mangaID),
			Title:     "Chapter 1",
			Order:     1,
			PageCount: 0,
		})
	}

	return items
}

func parsePhotoPagesHTML(sourceID string, chapterID string, baseURL string, page string) []Page {
	matches := reImgSrc.FindAllStringSubmatch(page, -1)
	items := make([]Page, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	base, _ := url.Parse(baseURL)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		imageURL := strings.TrimSpace(html.UnescapeString(match[1]))
		if !isLikelyPageImageURL(imageURL) {
			continue
		}
		if _, ok := seen[imageURL]; ok {
			continue
		}
		seen[imageURL] = struct{}{}

		parsed, err := url.Parse(imageURL)
		if err == nil && !parsed.IsAbs() && base != nil {
			imageURL = base.ResolveReference(parsed).String()
		}

		items = append(items, Page{
			SourceID:  sourceID,
			ChapterID: chapterID,
			ID:        chapterID + "-" + strconv.Itoa(len(items)),
			Index:     len(items),
			RemoteURL: imageURL,
		})
	}

	return items
}

func extractFirstMatch(text string, expressions ...*regexp.Regexp) string {
	for _, expression := range expressions {
		if expression == nil {
			continue
		}
		match := expression.FindStringSubmatch(text)
		if len(match) >= 2 {
			value := sanitizeText(match[1])
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func parseTextMatches(text string, expression *regexp.Regexp) []string {
	matches := expression.FindAllStringSubmatch(text, -1)
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := sanitizeText(match[1])
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func sanitizeText(value string) string {
	value = html.UnescapeString(value)
	value = stripTags(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func stripTags(value string) string {
	for {
		start := strings.IndexByte(value, '<')
		if start < 0 {
			return value
		}
		end := strings.IndexByte(value[start:], '>')
		if end < 0 {
			return value[:start]
		}
		value = value[:start] + value[start+end+1:]
	}
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func isLikelyPageImageURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "":
		return false
	case strings.Contains(value, ".jpg"),
		strings.Contains(value, ".jpeg"),
		strings.Contains(value, ".png"),
		strings.Contains(value, ".webp"),
		strings.Contains(value, ".avif"):
		return true
	case strings.Contains(value, "/media/albums/"),
		strings.Contains(value, "/media/photos/"),
		strings.Contains(value, "/photos/"):
		return true
	default:
		return false
	}
}

type httpCookieLike struct {
	Name  string
	Value string
}

type httpCookieList []*httpCookieLike

func (items httpCookieList) toHTTPCookies() []*http.Cookie {
	cookies := make([]*http.Cookie, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  item.Name,
			Value: item.Value,
			Path:  "/",
		})
	}
	return cookies
}
