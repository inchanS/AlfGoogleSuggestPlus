package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the real HTTP client and decoder against a controlled transport.
type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func TestQueryAndCache(t *testing.T) {
	for _, q := range []string{"서울 날씨", "google", "Go 한글", "a+b & 50%/#? ' \"", "한"} {
		t.Run(q, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("alfred_workflow_cache", dir)
			calls := 0
			c := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				params := r.URL.Query()
				if params.Get("q") != q || params.Get("oe") != "utf8" || params.Get("ie") != "utf8" || params.Get("output") != "chrome" {
					t.Fatal(r.URL)
				}
				data, _ := json.Marshal([]any{q, []string{"second", q, "", "first"}})
				return reply(200, string(data))
			})}
			for i := 0; i < 2; i++ {
				out := search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q)
				if !out.SkipKnowledge || len(out.Items) != 2 || out.Items[0].Arg != "second" || out.Items[1].Arg != "first" {
					t.Fatal(out)
				}
			}
			if calls != 1 {
				t.Fatal(calls)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
			if len(files) != 1 {
				t.Fatal(files)
			}
			old := time.Now().Add(-31 * time.Second)
			if err := os.Chtimes(files[0], old, old); err != nil {
				t.Fatal(err)
			}
			search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q)
			if calls != 2 {
				t.Fatal("expired cache used")
			}
			if err := os.WriteFile(files[0], []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q)
			if calls != 3 {
				t.Fatal("corrupt cache used")
			}
		})
	}
}
func TestFailureAndEmpty(t *testing.T) {
	for _, mode := range []string{"empty", "offline", "timeout", "status", "malformed", "wrong query", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("alfred_workflow_cache", t.TempDir())
			calls := 0
			c := &http.Client{Timeout: 40 * time.Millisecond, Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch mode {
				case "empty":
					return reply(200, `["q",[]]`)
				case "offline":
					return nil, errors.New("offline")
				case "timeout":
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "status":
					return reply(429, "limit")
				case "wrong query":
					return reply(200, `["old",["old suggestion"]]`)
				case "oversize":
					return reply(200, strings.Repeat(" ", maxResponse+1))
				default:
					return reply(200, "{")
				}
			})}
			if len(search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, " ").Items) != 0 || calls != 0 {
				t.Fatal("empty input")
			}
			for i := 0; i < 2; i++ {
				out := search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, "q")
				if len(out.Items) != 0 {
					t.Fatal(out)
				}
			}
			want := 2
			if mode == "empty" {
				want = 1
			}
			if calls != want {
				t.Fatal(calls)
			}
		})
	}
}
