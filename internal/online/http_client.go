package online

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"mynewmangaui/internal/config"
)

type httpClient struct {
	baseURL    *url.URL
	client     *http.Client
	userAgent  string
	rateLimit  time.Duration
	lastAccess time.Time
}

func newHTTPClient(cfg config.OnlineConfig, source config.OnlineSourceConfig) (*httpClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(source.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL := strings.TrimSpace(source.ProxyURL); proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy url: %w", err)
		}
		transport.Proxy = http.ProxyURL(parsedProxy)
	}

	timeout := time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		Jar:       jar,
	}

	return &httpClient{
		baseURL:   baseURL,
		client:    client,
		userAgent: strings.TrimSpace(source.UserAgent),
		rateLimit: time.Duration(source.RequestIntervalMs) * time.Millisecond,
	}, nil
}

func (c *httpClient) get(ctx context.Context, path string, query url.Values) ([]byte, *url.URL, error) {
	return c.do(ctx, http.MethodGet, path, query, nil)
}

func (c *httpClient) postForm(ctx context.Context, path string, form url.Values) ([]byte, *url.URL, error) {
	body := strings.NewReader(form.Encode())
	return c.do(ctx, http.MethodPost, path, nil, &requestBody{
		reader:      body,
		contentType: "application/x-www-form-urlencoded",
	})
}

func (c *httpClient) getAbsolute(ctx context.Context, rawURL string, accept string) ([]byte, string, error) {
	if c == nil || c.client == nil {
		return nil, "", fmt.Errorf("http client is not initialized")
	}

	targetURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", err
	}
	if !targetURL.IsAbs() && c.baseURL != nil {
		targetURL = c.baseURL.ResolveReference(targetURL)
	}

	var lastErr error
	var payload []byte
	var mime string
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.waitForRateLimit(ctx); err != nil {
			return nil, "", err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
		if err != nil {
			return nil, "", err
		}
		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if accept == "" {
			accept = "*/*"
		}
		req.Header.Set("Accept", accept)
		if c.baseURL != nil {
			req.Header.Set("Referer", c.baseURL.String())
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return nil, "", err
		}

		func() {
			defer resp.Body.Close()
			c.lastAccess = time.Now()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				errorPayload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
				if message := detectChallengeError(resp.StatusCode, errorPayload); message != "" {
					lastErr = fmt.Errorf("%s", message)
					return
				}
				lastErr = fmt.Errorf("upstream image request failed: %s (%s)", resp.Status, strings.TrimSpace(string(errorPayload)))
				return
			}

			readPayload, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				lastErr = readErr
				return
			}

			mime = strings.TrimSpace(resp.Header.Get("Content-Type"))
			if mime == "" {
				mime = "application/octet-stream"
			}
			payload = readPayload
			lastErr = nil
		}()

		if lastErr == nil {
			return payload, mime, nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
		}
	}

	return nil, "", lastErr
}

type requestBody struct {
	reader      io.Reader
	contentType string
}

func (c *httpClient) do(ctx context.Context, method string, path string, query url.Values, body *requestBody) ([]byte, *url.URL, error) {
	if c == nil || c.client == nil || c.baseURL == nil {
		return nil, nil, fmt.Errorf("http client is not initialized")
	}

	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, nil, err
	}

	endpoint := *c.baseURL
	endpoint.Path = joinURLPath(c.baseURL.Path, path)
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = body.reader
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, nil, err
	}

	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	if c.baseURL != nil {
		req.Header.Set("Referer", c.baseURL.String())
	}
	if body != nil && body.contentType != "" {
		req.Header.Set("Content-Type", body.contentType)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	c.lastAccess = time.Now()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if message := detectChallengeError(resp.StatusCode, payload); message != "" {
			return nil, resp.Request.URL, fmt.Errorf("%s", message)
		}
		return nil, resp.Request.URL, fmt.Errorf("upstream request failed: %s (%s)", resp.Status, strings.TrimSpace(string(payload)))
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Request.URL, err
	}
	return payload, resp.Request.URL, nil
}

func (c *httpClient) waitForRateLimit(ctx context.Context) error {
	if wait := c.rateLimit - time.Since(c.lastAccess); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func joinURLPath(basePath string, nextPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	nextPath = strings.TrimLeft(nextPath, "/")
	if basePath == "" {
		return "/" + nextPath
	}
	if nextPath == "" {
		return basePath
	}
	return basePath + "/" + nextPath
}

func detectChallengeError(statusCode int, payload []byte) string {
	body := strings.ToLower(string(payload))
	if statusCode == http.StatusForbidden && strings.Contains(body, "just a moment") && strings.Contains(body, "enable javascript and cookies to continue") {
		return "upstream blocked by Cloudflare challenge; set online.sources[].cookieHeader to a valid browser cookie string for 18comic"
	}
	return ""
}
