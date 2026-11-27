// Package cache is a small on-disk cache replacing wf.cached_data / cache_data.
// Entries are JSON files under Alfred's per-workflow cache directory, with
// freshness judged by file mtime.
package cache

import (
	"crypto/md5"
	"encoding/hex"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dir returns Alfred's workflow cache directory, or a temp fallback when the
// workflow env var is absent (e.g. local runs and tests).
func dir() string {
	if d := os.Getenv("alfred_workflow_cache"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "googlesuggestplus-cache")
}

func path(key string) string {
	return filepath.Join(dir(), key+".json")
}

// Key builds a filesystem-safe cache key: prefix + md5(word). The word may
// contain '/' or other path separators, so it is always hashed.
func Key(prefix, word string) string {
	sum := md5.Sum([]byte(word))
	return prefix + "_" + hex.EncodeToString(sum[:])
}

// Read returns the cached bytes for key when present and still fresh. A maxAge
// of 0 means "never expires": any existing entry is returned.
func Read(key string, maxAge time.Duration) ([]byte, bool) {
	p := path(key)
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false
	}
	if maxAge > 0 && time.Since(fi.ModTime()) > maxAge {
		return nil, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Write atomically replaces an entry. Readers see either complete version,
// including when another Script Filter process is stopped during a write.
func Write(key string, data []byte) error {
	d := dir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	prune(d)
	f, err := os.CreateTemp(d, ".cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(d, key+".json"))
}

// Sweep at most about once a day on cache writes, outside the cache-hit path.
func prune(dir string) {
	const retention = 24 * time.Hour
	stamp := filepath.Join(dir, ".last-cleanup")
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < retention {
		return
	}
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		return
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "google_") && !strings.HasPrefix(name, ".cache-") {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > retention {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// Load validates cached and fetched data using decode. Invalid entries are
// discarded and retried; invalid responses are never cached. A cache write
// failure is diagnostic only and must not discard a successful response.
func Load[T any](key string, maxAge time.Duration, loader func() ([]byte, error), decode func([]byte) (T, error)) (T, error) {
	if data, ok := Read(key, maxAge); ok {
		if value, err := decode(data); err == nil {
			return value, nil
		}
		// A successful fetch replaces this entry atomically.
	}
	var zero T
	data, err := loader()
	if err != nil {
		return zero, err
	}
	value, err := decode(data)
	if err != nil {
		return zero, err
	}
	if err := Write(key, data); err != nil {
		fmt.Fprintf(os.Stderr, "cache: could not save %s: %v\n", key, err)
	}
	return value, nil
}
