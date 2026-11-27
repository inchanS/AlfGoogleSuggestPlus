package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inchanS/AlfGoogleSuggestPlus/internal/cache"
)

func TestWeeklyCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("alfred_workflow_cache", dir)
	var calls atomic.Int32
	start := func() error { calls.Add(1); return nil }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); schedule(dir, start) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate check", calls.Load())
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	age := func(name string) {
		t.Helper()
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	age(".update-attempt")
	if err := cache.Write(releaseKey, []byte("v1.1.0")); err != nil {
		t.Fatal(err)
	}
	schedule(dir, start)
	if calls.Load() != 1 {
		t.Fatal("fresh release checked again")
	}
	age(releaseKey + ".json")
	schedule(dir, func() error { return errors.New("spawn failed") })
	schedule(dir, start)
	if calls.Load() != 2 {
		t.Fatal("weekly check did not restart")
	}
}

func TestReleaseInstall(t *testing.T) {
	var packageBody bytes.Buffer
	writer := zip.NewWriter(&packageBody)
	for _, name := range []string{"info.plist", "googlesuggest"} {
		member, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = member.Write([]byte("fixture")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, tag string
		status    int
		asset     string
		downloads int
		wantErr   bool
	}{
		{"upgrade", "v1.10.0", 200, assetName, 1, false},
		{"equal", "v1.9.0", 200, assetName, 0, false},
		{"older", "v1.8.0", 200, assetName, 0, false},
		{"bad tag", "v1.10.0-beta", 200, assetName, 0, true},
		{"missing asset", "v1.10.0", 200, "source.zip", 0, true},
		{"no releases", "", 404, "", 0, false},
		{"API unavailable", "", 503, "", 0, true},
		{"broken download", "v1.10.0", 200, assetName, 1, true},
		{"invalid archive", "v1.10.0", 200, assetName, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("alfred_workflow_cache", t.TempDir())
			t.Setenv("TMPDIR", t.TempDir())
			downloads := 0
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == releaseAPI {
					return reply(tc.status, fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":%q}]}`, tc.tag, tc.asset))
				}
				if r.URL.String() != "https://github.com/inchanS/AlfGoogleSuggestPlus/releases/download/"+tc.tag+"/"+assetName {
					t.Fatal(r.URL)
				}
				downloads++
				if tc.name == "broken download" {
					return reply(503, "failed")
				}
				if tc.name == "invalid archive" {
					return reply(200, "<html>Temporary error</html>")
				}
				return reply(200, packageBody.String())
			})}
			opened := false
			_, err := install(client, "1.9.0", func(p string) error {
				opened = true
				b, err := os.ReadFile(p)
				if !bytes.Equal(b, packageBody.Bytes()) {
					t.Fatal("incomplete download")
				}
				return err
			})
			if (err != nil) != tc.wantErr || downloads != tc.downloads || opened != (tc.downloads == 1 && !tc.wantErr) {
				t.Fatalf("downloads=%d opened=%v err=%v", downloads, opened, err)
			}
		})
	}
}
