package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/inchanS/AlfGoogleSuggestPlus/internal/cache"
)

const endpoint = "https://suggestqueries.google.com/complete/search"
const maxResponse = 1 << 20

type item struct {
	Title        string `json:"title"`
	Arg          string `json:"arg"`
	Autocomplete string `json:"autocomplete"`
	Subtitle     string `json:"subtitle"`
}

type feedback struct {
	Items         []item `json:"items"`
	SkipKnowledge bool   `json:"skipknowledge"`
}

func fetch(client *http.Client, endpoint, query string) ([]byte, error) {
	params := url.Values{"output": {"chrome"}, "ie": {"utf8"}, "oe": {"utf8"}, "q": {query}}
	resp, err := client.Get(endpoint + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if len(body) > maxResponse {
		return nil, errors.New("suggestion response too large")
	}
	return body, err
}

type suggestionLoader func(string) ([]byte, error)

func suggestions(load suggestionLoader, query string) ([]string, error) {
	return cache.Load(cache.Key("google", query), 30*time.Second, func() ([]byte, error) { return load(query) }, func(body []byte) ([]string, error) {
		var fields []json.RawMessage
		var words []string
		var echoed string
		if !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || len(fields) < 2 ||
			json.Unmarshal(fields[0], &echoed) != nil || echoed != query ||
			json.Unmarshal(fields[1], &words) != nil || words == nil {
			return nil, errors.New("invalid suggestion response")
		}
		return words, nil
	})
}

func search(load suggestionLoader, query string) feedback {
	out := feedback{Items: []item{}, SkipKnowledge: true}
	if strings.TrimSpace(query) == "" {
		return out
	}
	add := func(word string) {
		out.Items = append(out.Items, item{Title: word, Arg: word, Autocomplete: word, Subtitle: "Google에서 검색"})
	}
	// Alfred's native Keyword object supplies the actionable original query.
	words, err := suggestions(load, query)
	if err != nil {
		return out
	}
	for _, word := range words {
		if word != "" && word != query {
			add(word)
		}
	}
	return out
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--session" && os.Args[2] == "serve" {
		if err := serveSession(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}
	current := os.Getenv("alfred_workflow_version")
	if len(os.Args) == 3 && os.Args[1] == "--update" {
		switch os.Args[2] {
		case "check":
			_, err := checkRelease(&http.Client{Timeout: 15 * time.Second})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		case "install":
			message, err := install(&http.Client{Timeout: 2 * time.Minute}, current, func(path string) error { return exec.Command("/usr/bin/open", path).Run() })
			if err != nil {
				message = "업데이트 실패 · " + err.Error()
			}
			fmt.Print(message)
		}
		return
	}
	var query string
	if len(os.Args) > 1 {
		query = os.Args[1]
	}
	out := feedback{Items: []item{}, SkipKnowledge: true}
	if query == updateCommand {
		out.Items = append(out.Items, item{Title: "업데이트 확인 및 설치", Arg: updateCommand, Subtitle: "Enter로 최신 GitHub 릴리스를 확인합니다"})
	} else {
		dir := os.Getenv("alfred_workflow_cache")
		auto := dir != "" && current != "" && os.Getenv("automatic_updates") != "0"
		if auto {
			schedule(dir, startCheck)
		}
		if strings.TrimSpace(query) == "" {
			_, _ = sessionFetch("")
		} else {
			out = search(sessionFetch, query)
		}
		if auto {
			tag, _ := cache.Read(releaseKey, 0)
			if newer(string(tag), current) {
				out.Items = append([]item{{Title: "AlfGoogleSuggestPlus " + string(tag) + " 업데이트", Arg: updateCommand, Subtitle: "Enter로 내려받고 Alfred에서 설치"}}, out.Items...)
			}
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		os.Exit(1)
	}
}
