package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyBytes     = 2 << 20 // 2 MB
	searchLimit      = 5
	maxSourceTitle   = 200
	maxSourceSnippet = 500
	maxNestingDepth  = 3
)

var (
	ErrSearchNotConfigured = errors.New("search engine belum dikonfigurasi")
	ErrSearchUnavailable   = errors.New("search engine tidak mendukung metode POST maupun GET")
)

// Kunci yang dicek (berurutan) saat mencari daftar hasil di dalam JSON.
var listKeys = []string{"results", "data", "items", "sources", "organic", "organic_results", "hits", "web"}

type SearchClient struct {
	endpoint string
	apiKey   string
	limit    int
	http     *http.Client
}

func NewSearchClient(endpoint, apiKey string) *SearchClient {
	return &SearchClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		limit:    searchLimit,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Search mencoba POST (JSON) lebih dulu; bila dijawab 405, mencoba GET (?q=).
func (c *SearchClient) Search(ctx context.Context, query string) ([]Source, error) {
	if c.endpoint == "" {
		return nil, ErrSearchNotConfigured
	}

	for _, method := range []string{http.MethodPost, http.MethodGet} {
		sources, status, err := c.fetch(ctx, method, query)
		if err != nil {
			return nil, err
		}
		if status != http.StatusMethodNotAllowed {
			return sources, nil
		}
	}
	return nil, ErrSearchUnavailable
}

func (c *SearchClient) newRequest(ctx context.Context, method, query string) (*http.Request, error) {
	var body io.Reader
	if method == http.MethodPost {
		payload, _ := json.Marshal(map[string]any{
			"query": query, "q": query, "limit": c.limit, "max_results": c.limit,
		})
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint, body)
	if err != nil {
		return nil, err
	}

	if method == http.MethodGet {
		params := req.URL.Query()
		params.Set("q", query)
		params.Set("query", query)
		params.Set("limit", strconv.Itoa(c.limit))
		req.URL.RawQuery = params.Encode()
	} else {
		req.Header.Set("Content-Type", "application/json")
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *SearchClient) fetch(ctx context.Context, method, query string) ([]Source, int, error) {
	req, err := c.newRequest(ctx, method, query)
	if err != nil {
		return nil, 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, fmt.Errorf("search engine mengembalikan status %d", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&payload); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("respons search engine tidak valid: %w", err)
	}
	return parseSources(payload, c.limit), resp.StatusCode, nil
}

// findList mencari daftar hasil: array di puncak, atau di bawah salah satu listKeys (bersarang).
func findList(v any, depth int) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		if depth >= maxNestingDepth {
			return nil
		}
		for _, key := range listKeys {
			if found := findList(t[key], depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// parseSources mengubah JSON bebas bentuk menjadi daftar Source (unik, hanya URL http/https).
// Bila format asli API berbeda, sesuaikan fungsi ini.
func parseSources(payload any, limit int) []Source {
	sources := make([]Source, 0, limit)
	seen := map[string]bool{}

	for _, item := range findList(payload, 0) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		url := firstString(m, "url", "link", "href")
		if !isHTTPURL(url) || seen[url] {
			continue
		}
		seen[url] = true

		title := firstString(m, "title", "name")
		if title == "" {
			title = url
		}
		snippet := firstString(m, "snippet", "content", "description", "text", "summary")

		sources = append(sources, Source{
			Title:   truncateRunes(title, maxSourceTitle),
			URL:     url,
			Snippet: truncateRunes(snippet, maxSourceSnippet),
		})
		if len(sources) == limit {
			break
		}
	}
	return sources
}
