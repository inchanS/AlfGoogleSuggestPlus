package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
				out := search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q, q, "")
				if !out.SkipKnowledge || len(out.Items) != 3 || out.Items[0].Arg != q || out.Items[1].Arg != "second" || out.Items[2].Arg != "first" {
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
			search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q, q, "")
			if calls != 2 {
				t.Fatal("expired cache used")
			}
			if err := os.WriteFile(files[0], []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, q, q, "")
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
			for i := 0; i < 2; i++ {
				out := search(func(q string) ([]byte, error) { return fetch(c, endpoint, q) }, "q", "q", "")
				if len(out.Items) != 1 || out.Items[0].Arg != "q" {
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

func TestDirectSearchBeforeSuggestions(t *testing.T) {
	t.Setenv("alfred_workflow_cache", t.TempDir())
	calls := 0
	load := func(q string) ([]byte, error) {
		calls++
		return json.Marshal([]any{q, []string{q + " 날씨"}})
	}
	initial := search(load, "서울", "서", "")
	if calls != 0 || len(initial.Items) != 1 || initial.Items[0].Arg != "서울" || initial.Rerun != 0.1 || initial.Variables["pending_query"] != "서울" {
		t.Fatal(initial, calls)
	}
	full := search(load, "서울", initial.Variables["pending_query"], "")
	cached := search(load, "서울", "", "")
	for _, out := range []feedback{full, cached} {
		if calls != 1 || out.Rerun != 0 || len(out.Items) != 2 || out.Items[1].Autocomplete != "서울 날씨" {
			t.Fatal(out, calls)
		}
	}
}

func TestEmptyInputWarmsSessionAndOffersSearchEntry(t *testing.T) {
	for _, input := range []string{"", " "} {
		var requests []string
		out := search(func(q string) ([]byte, error) {
			requests = append(requests, q)
			return nil, errors.New("warmup unavailable")
		}, input, "", "")
		if len(requests) != 1 || requests[0] != "" || out.Rerun != 0 || len(out.Items) != 1 || out.Items[0].UID != "input" || out.Items[0].Arg != "" {
			t.Fatal(requests, out)
		}
	}
}

func TestPreviewWhileTyping(t *testing.T) {
	t.Setenv("alfred_workflow_cache", t.TempDir())
	load := func(q string) ([]byte, error) {
		return json.Marshal([]any{q, []string{"서울 날씨", "서울 내일", "서울역"}})
	}
	out := search(load, "서울", "서울", "")
	original := out.Items[1:]
	for _, q := range []string{"서울 ㄴ", "서울 나", "서울 날", "서울 날씨"} {
		out = search(load, q, out.Variables["pending_query"], out.Variables["suggestion_source"])
		if out.Rerun == 0 || out.Items[0].Arg != q || !slices.Equal(out.Items[1:], original) {
			t.Fatal(q, out)
		}
	}
	elsewhere := search(load, "부산", "", out.Variables["suggestion_source"])
	if len(elsewhere.Items) != 1 {
		t.Fatal(elsewhere)
	}
	for _, edited := range []string{"서울 날", "서울역", ""} {
		changed := search(load, edited, out.Variables["pending_query"], out.Variables["suggestion_source"])
		if len(changed.Items) != 1 || changed.Variables["suggestion_source"] != "" {
			t.Fatal(edited, changed)
		}
	}
	// A confirmed empty result removes the preview.
	full := search(func(q string) ([]byte, error) { return json.Marshal([]any{q, []string{}}) }, "서울 날씨", out.Variables["pending_query"], out.Variables["suggestion_source"])
	if len(full.Items) != 1 || full.Rerun != 0 {
		t.Fatal(full)
	}
}

func TestHangulInputContinuation(t *testing.T) {
	for _, pair := range [][2]string{{"ㄴ", "나"}, {"나", "날"}, {"고", "과"}, {"설", "서루"}, {"읽", "일거"}} {
		if !continuesInput(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	for _, pair := range [][2]string{{"날", "나"}, {"서울", "부산"}, {"과", "고"}, {"hello", "help"}} {
		if continuesInput(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
}

func TestAlfredDecomposedKeyboardInput(t *testing.T) {
	t.Setenv("alfred_workflow_cache", t.TempDir())
	inputs := []string{"ㅍ", "펴", "평", "평ㅅ", "평새", "평생", "평생ㄱ", "평생교", "평생교ㅇ", "평생교유", "평생교육"}
	load := func(q string) ([]byte, error) {
		return json.Marshal([]any{q, []string{"평생교육", "평생교육원", "평생교육원"}})
	}
	out := search(load, inputs[0], inputs[0], "")
	for _, q := range inputs[1:] {
		previous := out.Items[1:]
		out = search(load, q, out.Variables["pending_query"], out.Variables["suggestion_source"])
		if !slices.Equal(previous, out.Items[1:]) || len(previous) != 2 {
			t.Fatal(q, out)
		}
		out = search(load, q, out.Variables["pending_query"], out.Variables["suggestion_source"])
	}
	if len(out.Items) != 2 || out.Items[1].Title != "평생교육원" {
		t.Fatal(out)
	}
}
