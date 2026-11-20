package capture

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// TitleTimeout is the total budget of a title fetch.
const TitleTimeout = 2 * time.Second

const (
	maxTitleBody  = 512 << 10
	maxTitleRunes = 200
)

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// FetchTitle downloads rawURL (at most 512 KB, within timeout) and returns
// its <title>. Any failure returns an error; callers fall back to the URL.
func FetchTitle(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = TitleTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "stkr-bookmark/1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return errors.New("redirect to unsupported scheme")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch title: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetch title: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTitleBody))
	if err != nil {
		return "", fmt.Errorf("fetch title: %w", err)
	}
	return ParseTitle(string(body)), nil
}

// ParseTitle extracts and cleans the first <title> of an HTML document.
func ParseTitle(doc string) string {
	m := titleRe.FindStringSubmatch(doc)
	if m == nil {
		return ""
	}
	t := strings.Join(strings.Fields(html.UnescapeString(m[1])), " ")
	t = strings.ToValidUTF8(t, "")
	if utf8.RuneCountInString(t) > maxTitleRunes {
		t = string([]rune(t)[:maxTitleRunes])
	}
	return t
}
