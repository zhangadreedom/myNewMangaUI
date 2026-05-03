package online

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const (
	legacy18ComicTokenSecret      = "18comicAPP"
	legacy18ComicContentSecret    = "18comicAPPContent"
	legacy18ComicDataSecret       = "185Hcomic3PAPP7R"
	legacy18ComicAppVersion       = "2.0.19"
	legacy18ComicImageAccept      = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"
	legacy18ComicDefaultImageMIME = "image/png"
	legacy18ComicDefaultScramble  = 220980
)

var (
	default18ComicAPIBaseURLs = []string{
		"https://www.cdnhth.club",
		"https://www.cdngwc.cc",
		"https://www.cdnhth.net",
		"https://www.cdnbea.net",
		"https://www.cdnaspa.vip",
		"https://www.cdnaspa.club",
		"https://www.cdnplaystation6.vip",
		"https://www.cdnplaystation6.cc",
	}
	default18ComicImageBaseURLs = []string{
		"https://cdn-msp.jmapiproxy1.cc",
		"https://cdn-msp.jmapiproxy2.cc",
		"https://cdn-msp2.jmapiproxy2.cc",
		"https://cdn-msp3.jmapiproxy2.cc",
		"https://cdn-msp.jmapinodeudzn.net",
		"https://cdn-msp3.jmapinodeudzn.net",
		"https://cdn-msp.jmapiproxy3.cc",
		"https://cdn-msp.jmdanjonproxy.xyz",
	}
	re18ComicScrambleID = regexp.MustCompile(`(?i)\bscramble(?:_id|Id)?\b["':=\s]+(\d+)`)
)

type comic18SearchPayload struct {
	Total   any                     `json:"total"`
	Content []comic18SearchItemJSON `json:"content"`
}

type comic18SearchItemJSON struct {
	ID          any `json:"id"`
	Name        any `json:"name"`
	Image       any `json:"image"`
	Author      any `json:"author"`
	Description any `json:"description"`
	Tags        any `json:"tags"`
}

type comic18AlbumPayload struct {
	ID          any                     `json:"id"`
	Name        any                     `json:"name"`
	Description any                     `json:"description"`
	Author      any                     `json:"author"`
	Tags        any                     `json:"tags"`
	Series      []comic18SeriesItemJSON `json:"series"`
	RelatedList []comic18SearchItemJSON `json:"related_list"`
	Images      []string                `json:"images"`
}

type comic18SeriesItemJSON struct {
	ID   any `json:"id"`
	Name any `json:"name"`
	Sort any `json:"sort"`
}

type comic18ChapterPayload struct {
	ID       any                     `json:"id"`
	Name     any                     `json:"name"`
	SeriesID any                     `json:"series_id"`
	Series   []comic18SeriesItemJSON `json:"series"`
	Images   []string                `json:"images"`
	Tags     any                     `json:"tags"`
}

func (p *Provider18Comic) searchMobile(ctx context.Context, options SearchOptions) ([]Manga, error) {
	if err := p.ensureSession(ctx); err != nil {
		return nil, err
	}

	queryText := strings.TrimSpace(options.Query)

	query := url.Values{}
	query.Set("search_query", queryText)
	query.Set("o", "mr")
	if options.Page > 1 {
		query.Set("page", strconv.Itoa(options.Page))
	}

	var payload comic18SearchPayload
	if err := p.mobileAPIGet(ctx, "/search", query, false, &payload); err != nil {
		return nil, err
	}

	items := make([]Manga, 0, len(payload.Content))
	for _, item := range payload.Content {
		mangaID := strings.TrimSpace(stringify18Comic(item.ID))
		if mangaID == "" {
			continue
		}

		tags := uniqueStrings(normalize18ComicTags(item.Tags))
		manga := Manga{
			SourceID: p.source.ID,
			ID:       mangaID,
			Title:    fallback18ComicTitle(item.Name, mangaID),
			CoverURL: p.coverURL(mangaID, stringify18Comic(item.Image), "_3x4"),
			Author:   join18ComicPeople(item.Author),
			Tags:     tags,
		}
		items = append(items, manga)
	}

	return items, nil
}

func (p *Provider18Comic) getMangaMobile(ctx context.Context, mangaID string) (Manga, error) {
	if err := p.ensureSession(ctx); err != nil {
		return Manga{}, err
	}

	mangaID = strings.TrimSpace(mangaID)
	if mangaID == "" {
		return Manga{}, fmt.Errorf("manga id is required")
	}

	payload, err := p.fetchAlbumPayload(ctx, mangaID)
	if err != nil {
		return Manga{}, err
	}

	return Manga{
		SourceID: p.source.ID,
		ID:       mangaID,
		Title:    fallback18ComicTitle(payload.Name, mangaID),
		CoverURL: p.coverURL(mangaID, "", ""),
		Author:   join18ComicPeople(payload.Author),
		Tags:     uniqueStrings(normalize18ComicTags(payload.Tags)),
	}, nil
}

func (p *Provider18Comic) getChaptersMobile(ctx context.Context, mangaID string) ([]Chapter, error) {
	if err := p.ensureSession(ctx); err != nil {
		return nil, err
	}

	mangaID = strings.TrimSpace(mangaID)
	if mangaID == "" {
		return nil, fmt.Errorf("manga id is required")
	}

	payload, err := p.fetchAlbumPayload(ctx, mangaID)
	if err != nil {
		return nil, err
	}

	items := make([]Chapter, 0, max18ComicInt(len(payload.Series), 1))
	for _, series := range payload.Series {
		chapterID := strings.TrimSpace(stringify18Comic(series.ID))
		if chapterID == "" {
			continue
		}
		items = append(items, Chapter{
			SourceID:  p.source.ID,
			MangaID:   mangaID,
			ID:        chapterID,
			Title:     fallback18ComicChapterTitle(series.Name, len(items)+1),
			Order:     normalize18ComicOrder(series.Sort, len(items)+1),
			PageCount: 0,
		})
	}

	if len(items) == 0 {
		items = append(items, Chapter{
			SourceID:  p.source.ID,
			MangaID:   mangaID,
			ID:        mangaID,
			Title:     fallback18ComicChapterTitle(payload.Name, 1),
			Order:     1,
			PageCount: len(payload.Images),
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

func (p *Provider18Comic) getPagesMobile(ctx context.Context, chapterID string) ([]Page, error) {
	if err := p.ensureSession(ctx); err != nil {
		return nil, err
	}

	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return nil, fmt.Errorf("chapter id is required")
	}

	payload, err := p.fetchChapterPayload(ctx, chapterID)
	if err != nil {
		return nil, err
	}

	scrambleID, err := p.fetchChapterScrambleID(ctx, chapterID)
	if err != nil {
		return nil, err
	}

	items := make([]Page, 0, len(payload.Images))
	for index, name := range payload.Images {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		remoteURL := p.buildPageImageURL(chapterID, name, scrambleID)
		items = append(items, Page{
			SourceID:  p.source.ID,
			ChapterID: chapterID,
			ID:        chapterID + "-" + strconv.Itoa(index),
			Index:     index,
			RemoteURL: remoteURL,
		})
	}

	return items, nil
}

func (p *Provider18Comic) fetchMobileImage(ctx context.Context, remoteURL string) ([]byte, string, error) {
	if err := p.ensureSession(ctx); err != nil {
		return nil, "", err
	}

	targetURL, decodeMeta, err := parse18ComicImageTarget(remoteURL)
	if err != nil {
		return nil, "", err
	}

	var payload []byte
	var mimeType string
	var lastErr error
	for _, candidate := range p.imageURLCandidates(targetURL) {
		nextPayload, nextMime, err := p.fetchMobileImageBytes(ctx, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		payload = nextPayload
		mimeType = nextMime
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	if len(payload) == 0 {
		return nil, "", fmt.Errorf("18comic image request returned empty payload")
	}

	if !decodeMeta.Enabled {
		return payload, mimeType, nil
	}

	decoded, err := decode18ComicImage(payload, decodeMeta)
	if err != nil {
		return nil, "", err
	}

	return decoded, legacy18ComicDefaultImageMIME, nil
}

func (p *Provider18Comic) fetchAlbumPayload(ctx context.Context, mangaID string) (comic18AlbumPayload, error) {
	query := url.Values{}
	query.Set("comicName", "")
	query.Set("id", mangaID)

	var payload comic18AlbumPayload
	if err := p.mobileAPIGet(ctx, "/album", query, false, &payload); err != nil {
		return comic18AlbumPayload{}, err
	}
	return payload, nil
}

func (p *Provider18Comic) fetchChapterPayload(ctx context.Context, chapterID string) (comic18ChapterPayload, error) {
	query := url.Values{}
	query.Set("comicName", "")
	query.Set("skip", "")
	query.Set("id", chapterID)

	var payload comic18ChapterPayload
	if err := p.mobileAPIGet(ctx, "/chapter", query, false, &payload); err != nil {
		return comic18ChapterPayload{}, err
	}
	return payload, nil
}

func (p *Provider18Comic) fetchChapterScrambleID(ctx context.Context, chapterID string) (int, error) {
	query := url.Values{}
	query.Set("id", chapterID)
	query.Set("mode", "vertical")
	query.Set("page", "0")
	query.Set("app_img_shunt", "1")
	query.Set("express", "off")

	body, ts, err := p.mobileAPIRawPayloadWithTimestamp(ctx, "/chapter_view_template", query, true)
	if err != nil {
		return 0, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		payload, err := decode18ComicAPIEnvelope(trimmed, ts)
		if err == nil {
			if scrambleID := parse18ComicScrambleID(string(payload)); scrambleID > 0 {
				return scrambleID, nil
			}
		}
	}

	if scrambleID := parse18ComicScrambleID(string(trimmed)); scrambleID > 0 {
		return scrambleID, nil
	}

	return legacy18ComicDefaultScramble, nil
}

func (p *Provider18Comic) mobileAPIGet(ctx context.Context, apiPath string, query url.Values, contentToken bool, out any) error {
	payload, err := p.mobileAPIPayload(ctx, apiPath, query, contentToken)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode 18comic mobile response for %s: %w", apiPath, err)
	}
	return nil
}

func (p *Provider18Comic) mobileAPIPayload(ctx context.Context, apiPath string, query url.Values, contentToken bool) ([]byte, error) {
	body, ts, err := p.mobileAPIRawPayloadWithTimestamp(ctx, apiPath, query, contentToken)
	if err != nil {
		return nil, err
	}
	return decode18ComicAPIEnvelope(body, ts)
}

func (p *Provider18Comic) mobileAPIRawPayload(ctx context.Context, apiPath string, query url.Values, contentToken bool) ([]byte, error) {
	body, _, err := p.mobileAPIRawPayloadWithTimestamp(ctx, apiPath, query, contentToken)
	return body, err
}

func (p *Provider18Comic) mobileAPIRawPayloadWithTimestamp(ctx context.Context, apiPath string, query url.Values, contentToken bool) ([]byte, int64, error) {
	apiBases := p.mobileAPIBaseURLs()
	if len(apiBases) == 0 {
		return nil, 0, fmt.Errorf("18comic mobile api is not configured")
	}

	var lastErr error
	for _, base := range apiBases {
		payload, ts, err := p.mobileAPIRawPayloadFromBase(ctx, base, apiPath, query, contentToken)
		if err == nil {
			return payload, ts, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("18comic mobile api request failed")
	}
	return nil, 0, lastErr
}

func (p *Provider18Comic) mobileAPIRawPayloadFromBase(ctx context.Context, base *url.URL, apiPath string, query url.Values, contentToken bool) ([]byte, int64, error) {
	if p == nil || p.client == nil || p.client.client == nil {
		return nil, 0, fmt.Errorf("18comic provider is not initialized")
	}

	if err := p.client.waitForRateLimit(ctx); err != nil {
		return nil, 0, err
	}

	endpoint := *base
	endpoint.Path = joinURLPath(base.Path, apiPath)
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	now := time.Now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, err
	}

	for key, value := range p.mobileHeaders(now, contentToken) {
		req.Header.Set(key, value)
	}

	resp, err := p.client.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	p.client.lastAccess = time.Now()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message := detectChallengeError(resp.StatusCode, body); message != "" {
			return nil, 0, fmt.Errorf("%s", message)
		}
		return nil, 0, fmt.Errorf("18comic mobile api request failed: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
	}

	return body, now, nil
}

func (p *Provider18Comic) mobileHeaders(now int64, contentToken bool) map[string]string {
	tokenSecret := legacy18ComicTokenSecret
	if contentToken {
		tokenSecret = legacy18ComicContentSecret
	}

	tokenparam := fmt.Sprintf("%d,%s", now, legacy18ComicAppVersion)
	token := md5Hex18Comic(fmt.Sprintf("%d%s", now, tokenSecret))
	headers := map[string]string{
		"Accept":           "application/json,text/plain,*/*",
		"Accept-Language":  "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		"tokenparam":       tokenparam,
		"token":            token,
		"user-agent":       "Mozilla/5.0 (Linux; Android 9; V1938CT Build/PQ3A.190705.11211812; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/91.0.4472.114 Safari/537.36",
		"version":          legacy18ComicAppVersion,
		"X-Requested-With": "com.JMComic3.app",
	}
	if p.client != nil && p.client.baseURL != nil {
		headers["Referer"] = p.client.baseURL.String()
	}
	return headers
}

func (p *Provider18Comic) mobileAPIBaseURLs() []*url.URL {
	items := make([]*url.URL, 0, len(default18ComicAPIBaseURLs))
	seen := make(map[string]struct{}, len(default18ComicAPIBaseURLs))
	for _, raw := range default18ComicAPIBaseURLs {
		parsed, err := url.Parse(raw)
		if err != nil || parsed == nil || parsed.Host == "" {
			continue
		}
		key := parsed.Scheme + "://" + parsed.Host
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, parsed)
	}
	return items
}

func (p *Provider18Comic) imageBaseURLs() []*url.URL {
	items := make([]*url.URL, 0, len(default18ComicImageBaseURLs))
	seen := make(map[string]struct{}, len(default18ComicImageBaseURLs))
	for _, raw := range default18ComicImageBaseURLs {
		parsed, err := url.Parse(raw)
		if err != nil || parsed == nil || parsed.Host == "" {
			continue
		}
		key := parsed.Scheme + "://" + parsed.Host
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, parsed)
	}
	return items
}

func (p *Provider18Comic) coverURL(mangaID string, remoteImage string, sizeSuffix string) string {
	remoteImage = strings.TrimSpace(remoteImage)
	if remoteImage != "" {
		if parsed, err := url.Parse(remoteImage); err == nil {
			if parsed.IsAbs() {
				return parsed.String()
			}
		}
	}

	bases := p.imageBaseURLs()
	if len(bases) == 0 {
		return remoteImage
	}
	base := bases[int(crc32.ChecksumIEEE([]byte(mangaID)))%len(bases)]
	target := *base
	target.Path = joinURLPath(base.Path, "/media/albums/"+mangaID+sizeSuffix+".jpg")
	return target.String()
}

func (p *Provider18Comic) imageURLCandidates(targetURL *url.URL) []*url.URL {
	if targetURL == nil {
		return nil
	}

	items := []*url.URL{targetURL}
	seen := map[string]struct{}{targetURL.Scheme + "://" + targetURL.Host: {}}
	for _, base := range p.imageBaseURLs() {
		if base == nil || base.Host == "" {
			continue
		}
		key := base.Scheme + "://" + base.Host
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		candidate := *base
		candidate.Path = joinURLPath(base.Path, targetURL.Path)
		candidate.RawQuery = targetURL.RawQuery
		items = append(items, &candidate)
	}
	return items
}

func (p *Provider18Comic) buildPageImageURL(chapterID string, filename string, scrambleID int) string {
	bases := p.imageBaseURLs()
	if len(bases) == 0 {
		return ""
	}

	key := chapterID + "/" + filename
	base := bases[int(crc32.ChecksumIEEE([]byte(key)))%len(bases)]
	target := *base
	target.Path = joinURLPath(base.Path, "/media/photos/"+chapterID+"/"+filename)
	query := target.Query()
	query.Set("jm_decode", "1")
	query.Set("jm_aid", chapterID)
	query.Set("jm_scramble_id", strconv.Itoa(scrambleID))
	query.Set("jm_file", filename)
	target.RawQuery = query.Encode()
	return target.String()
}

func (p *Provider18Comic) fetchMobileImageBytes(ctx context.Context, targetURL *url.URL) ([]byte, string, error) {
	if p == nil || p.client == nil || p.client.client == nil {
		return nil, "", fmt.Errorf("18comic provider is not initialized")
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := p.client.waitForRateLimit(ctx); err != nil {
			return nil, "", err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
		if err != nil {
			return nil, "", err
		}

		for key, value := range map[string]string{
			"Accept":           legacy18ComicImageAccept,
			"Accept-Encoding":  "identity",
			"Accept-Language":  "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
			"user-agent":       "Mozilla/5.0 (Linux; Android 9; V1938CT Build/PQ3A.190705.11211812; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/91.0.4472.114 Safari/537.36",
			"X-Requested-With": "com.JMComic3.app",
		} {
			req.Header.Set(key, value)
		}
		if p.client.baseURL != nil {
			req.Header.Set("Referer", p.client.baseURL.String())
		}

		resp, err := p.client.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return nil, "", err
		}

		payload, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		p.client.lastAccess = time.Now()
		if readErr != nil {
			lastErr = readErr
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return nil, "", readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if message := detectChallengeError(resp.StatusCode, payload); message != "" {
				return nil, "", fmt.Errorf("%s", message)
			}
			lastErr = fmt.Errorf("18comic image request failed: %s (%s)", resp.Status, strings.TrimSpace(string(payload)))
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return nil, "", lastErr
		}

		mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if !is18ComicImagePayload(payload, mimeType) {
			lastErr = fmt.Errorf("18comic image request returned non-image payload: %s (%d bytes)", mimeType, len(payload))
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return nil, "", lastErr
		}
		return payload, mimeType, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("18comic image request failed")
	}
	return nil, "", lastErr
}

func is18ComicImagePayload(payload []byte, mimeType string) bool {
	if len(payload) == 0 {
		return false
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	return has18ComicImageSignature(payload)
}

func has18ComicImageSignature(payload []byte) bool {
	if len(payload) >= 3 && payload[0] == 0xff && payload[1] == 0xd8 && payload[2] == 0xff {
		return true
	}
	if len(payload) >= 8 && string(payload[:8]) == "\x89PNG\r\n\x1a\n" {
		return true
	}
	if len(payload) >= 6 && (string(payload[:6]) == "GIF87a" || string(payload[:6]) == "GIF89a") {
		return true
	}
	if len(payload) >= 12 && string(payload[:4]) == "RIFF" && string(payload[8:12]) == "WEBP" {
		return true
	}
	return false
}

func decode18ComicAPIEnvelope(body []byte, ts int64) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse 18comic mobile envelope: %w", err)
	}

	if code := parse18ComicAPIStatus(envelope["code"]); code != 0 && code != 200 {
		return nil, fmt.Errorf("18comic mobile api error: %s", parse18ComicAPIMessage(envelope))
	}

	data := bytes.TrimSpace(envelope["data"])
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, fmt.Errorf("18comic mobile api returned empty data")
	}

	if data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return nil, fmt.Errorf("decode 18comic encrypted payload: %w", err)
		}
		return decode18ComicResponseData(encoded, ts)
	}

	return data, nil
}

func parse18ComicAPIStatus(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}

	var numeric int
	if err := json.Unmarshal(raw, &numeric); err == nil {
		return numeric
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, _ := strconv.Atoi(strings.TrimSpace(text))
		return value
	}

	return 0
}

func parse18ComicAPIMessage(envelope map[string]json.RawMessage) string {
	for _, key := range []string{"msg", "message", "errorMsg", "error_message"} {
		raw := bytes.TrimSpace(envelope[key])
		if len(raw) == 0 {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return "unknown upstream error"
}

func decode18ComicResponseData(encoded string, ts int64) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode 18comic response: %w", err)
	}

	key := []byte(md5Hex18Comic(fmt.Sprintf("%d%s", ts, legacy18ComicDataSecret)))
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create 18comic aes cipher: %w", err)
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("18comic encrypted payload has invalid block size")
	}

	plaintext := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += block.BlockSize() {
		block.Decrypt(plaintext[offset:offset+block.BlockSize()], ciphertext[offset:offset+block.BlockSize()])
	}

	if len(plaintext) == 0 {
		return nil, fmt.Errorf("18comic encrypted payload is empty")
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding <= 0 || padding > len(plaintext) {
		return nil, fmt.Errorf("18comic encrypted payload padding is invalid")
	}

	return plaintext[:len(plaintext)-padding], nil
}

func md5Hex18Comic(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

type comic18ImageDecodeMeta struct {
	Enabled    bool
	Aid        int
	ScrambleID int
	Filename   string
}

func parse18ComicImageTarget(raw string) (*url.URL, comic18ImageDecodeMeta, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, comic18ImageDecodeMeta{}, err
	}

	query := parsed.Query()
	meta := comic18ImageDecodeMeta{
		Enabled:    query.Get("jm_decode") == "1",
		Aid:        parse18ComicStringInt(query.Get("jm_aid")),
		ScrambleID: parse18ComicStringInt(query.Get("jm_scramble_id")),
		Filename:   strings.TrimSpace(query.Get("jm_file")),
	}

	if meta.Filename == "" {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) > 0 {
			meta.Filename = segments[len(segments)-1]
		}
	}
	if meta.Aid == 0 {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) >= 3 && segments[len(segments)-2] != "" {
			meta.Aid = parse18ComicStringInt(segments[len(segments)-2])
		}
	}

	for _, key := range []string{"jm_decode", "jm_aid", "jm_scramble_id", "jm_file"} {
		query.Del(key)
	}
	parsed.RawQuery = query.Encode()

	return parsed, meta, nil
}

func decode18ComicImage(payload []byte, meta comic18ImageDecodeMeta) ([]byte, error) {
	segments := segmentationCount18Comic(meta.ScrambleID, meta.Aid, meta.Filename)
	if segments <= 0 {
		return payload, nil
	}

	source, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("decode 18comic image: %w", err)
	}

	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("18comic image bounds are invalid")
	}

	target := image.NewRGBA(bounds)
	remainder := height % segments
	sliceHeight := height / segments

	for index := 0; index < segments; index++ {
		move := sliceHeight
		ySrc := height - (sliceHeight * (index + 1)) - remainder
		yDst := sliceHeight * index
		if index == 0 {
			move += remainder
		} else {
			yDst += remainder
		}

		rect := image.Rect(bounds.Min.X, bounds.Min.Y+ySrc, bounds.Min.X+width, bounds.Min.Y+ySrc+move)
		draw18ComicImage(target, image.Rect(bounds.Min.X, bounds.Min.Y+yDst, bounds.Min.X+width, bounds.Min.Y+yDst+move), source, rect.Min)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, target); err != nil {
		return nil, fmt.Errorf("encode 18comic decoded image: %w", err)
	}
	return buf.Bytes(), nil
}

func draw18ComicImage(dst *image.RGBA, rect image.Rectangle, src image.Image, srcPoint image.Point) {
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			dst.Set(rect.Min.X+x, rect.Min.Y+y, src.At(srcPoint.X+x, srcPoint.Y+y))
		}
	}
}

func segmentationCount18Comic(scrambleID int, aid int, filename string) int {
	if scrambleID <= 0 || aid <= 0 || strings.TrimSpace(filename) == "" {
		return 0
	}
	if aid < scrambleID {
		return 0
	}
	if aid < 268850 {
		return 10
	}

	modulo := 10
	if aid >= 421926 {
		modulo = 8
	}

	name := strings.TrimSpace(filepath.Base(filename))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" {
		return 0
	}

	hash := md5Hex18Comic(strconv.Itoa(aid) + name)
	if len(hash) == 0 {
		return 0
	}
	value := int(hash[len(hash)-1])
	value %= modulo
	return value*2 + 2
}

func parse18ComicScrambleID(payload any) int {
	switch value := payload.(type) {
	case map[string]any:
		for _, key := range []string{"scramble_id", "scrambleId"} {
			if item, ok := value[key]; ok {
				if parsed := parse18ComicAnyInt(item); parsed > 0 {
					return parsed
				}
			}
		}
		for _, item := range value {
			if parsed := parse18ComicScrambleID(item); parsed > 0 {
				return parsed
			}
		}
	case []any:
		for _, item := range value {
			if parsed := parse18ComicScrambleID(item); parsed > 0 {
				return parsed
			}
		}
	case string:
		match := re18ComicScrambleID.FindStringSubmatch(value)
		if len(match) >= 2 {
			return parse18ComicStringInt(match[1])
		}
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}

func stringify18Comic(value any) string {
	switch item := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(item)
	case json.Number:
		return item.String()
	case float64:
		return strconv.FormatInt(int64(item), 10)
	case int:
		return strconv.Itoa(item)
	case int64:
		return strconv.FormatInt(item, 10)
	case []string:
		return strings.Join(item, ", ")
	case []any:
		values := make([]string, 0, len(item))
		for _, entry := range item {
			if text := stringify18Comic(entry); text != "" {
				values = append(values, text)
			}
		}
		return strings.Join(values, ", ")
	default:
		return strings.TrimSpace(fmt.Sprint(item))
	}
}

func join18ComicPeople(value any) string {
	switch item := value.(type) {
	case []any:
		people := make([]string, 0, len(item))
		for _, entry := range item {
			if text := strings.TrimSpace(stringify18Comic(entry)); text != "" {
				people = append(people, text)
			}
		}
		return strings.Join(uniqueStrings(people), ", ")
	case []string:
		return strings.Join(uniqueStrings(item), ", ")
	default:
		return strings.TrimSpace(stringify18Comic(value))
	}
}

func normalize18ComicTags(value any) []string {
	switch item := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(item) == "" {
			return nil
		}
		return strings.Fields(strings.ReplaceAll(item, ",", " "))
	case []string:
		return item
	case []any:
		tags := make([]string, 0, len(item))
		for _, entry := range item {
			if text := strings.TrimSpace(stringify18Comic(entry)); text != "" {
				tags = append(tags, text)
			}
		}
		return tags
	default:
		text := strings.TrimSpace(stringify18Comic(item))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func fallback18ComicTitle(value any, fallback string) string {
	title := strings.TrimSpace(stringify18Comic(value))
	if title != "" {
		return title
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "Untitled"
}

func fallback18ComicChapterTitle(value any, order int) string {
	title := strings.TrimSpace(stringify18Comic(value))
	if title != "" {
		return title
	}
	return "Chapter " + strconv.Itoa(max18ComicInt(order, 1))
}

func normalize18ComicOrder(value any, fallback int) int {
	if parsed := parse18ComicAnyInt(value); parsed > 0 {
		return parsed
	}
	return max18ComicInt(fallback, 1)
}

func parse18ComicAnyInt(value any) int {
	switch item := value.(type) {
	case nil:
		return 0
	case int:
		return item
	case int64:
		return int(item)
	case float64:
		return int(item)
	case json.Number:
		parsed, _ := item.Int64()
		return int(parsed)
	case string:
		return parse18ComicStringInt(item)
	default:
		return parse18ComicStringInt(fmt.Sprint(item))
	}
}

func parse18ComicStringInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func max18ComicInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
