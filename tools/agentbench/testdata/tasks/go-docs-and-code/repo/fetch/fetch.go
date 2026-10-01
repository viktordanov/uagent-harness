// Package fetch downloads URLs with the configured settings.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"example.com/fetcher/config"
)

// Getter does one attempt; tests replace it.
type Getter func(ctx context.Context, url string) ([]byte, error)

// HTTPGetter is the real Getter for a config.
func HTTPGetter(c config.Config) Getter {
	client := &http.Client{Timeout: c.Timeout}

	return func(ctx context.Context, url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", url, resp.Status)
		}

		return io.ReadAll(resp.Body)
	}
}

// Fetch gets url with get.
func Fetch(ctx context.Context, c config.Config, get Getter, url string) ([]byte, error) {
	body, err := get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	return body, nil
}
