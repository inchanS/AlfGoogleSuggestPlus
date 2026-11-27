package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

func suggestions(client *http.Client, endpoint, query string) ([]string, error) {
	return cache.Load(cache.Key("google", query), 30*time.Second, func() ([]byte, error) { return fetch(client, endpoint, query) }, func(body []byte) ([]string, error) {
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

func search(client *http.Client, endpoint, query string) feedback {
	out := feedback{Items: []item{}, SkipKnowledge: true}
	if strings.TrimSpace(query) == "" {
		return out
	}
	add := func(word string) {
		out.Items = append(out.Items, item{Title: word, Arg: word, Autocomplete: word, Subtitle: "Google에서 검색"})
	}
	// Alfred's native Keyword object supplies the actionable original query.
	words, err := suggestions(client, endpoint, query)
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
	var query string
	if len(os.Args) > 1 {
		query = os.Args[1]
	}
	out := search(&http.Client{Timeout: 1500 * time.Millisecond}, endpoint, query)
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		os.Exit(1)
	}
}
