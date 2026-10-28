package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const downloadWorkers = 4

func cacheVersion(version string) string {
	if version == "" {
		return "unknown"
	}
	return version
}

// download fetches images that are not already on disk, in parallel,
// marking each one that fails. The URLs point at Figma's image CDN, so no
// token is sent.
func (s *Session) download(ctx context.Context, images []*Image) {
	var mu sync.Mutex
	sem := make(chan struct{}, downloadWorkers)
	var wg sync.WaitGroup
	for _, img := range images {
		if info, err := os.Stat(img.Path); err == nil && info.Size() > 0 {
			img.Bytes = info.Size()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(img *Image) {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := s.fetchToFile(ctx, img.URL, img.Path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				img.failed = true
				return
			}
			img.Bytes, img.Downloaded = n, true
		}(img)
	}
	wg.Wait()
}

func (s *Session) fetchToFile(ctx context.Context, url, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.Client.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), path)
}
