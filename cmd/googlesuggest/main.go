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
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/inchanS/AlfGoogleSuggestPlus/internal/cache"
)

const endpoint = "https://suggestqueries.google.com/complete/search"
const maxResponse = 1 << 20

type item struct {
	UID          string `json:"uid"`
	Title        string `json:"title"`
	Arg          string `json:"arg"`
	Autocomplete string `json:"autocomplete"`
	Subtitle     string `json:"subtitle"`
}

type feedback struct {
	Rerun         float64           `json:"rerun,omitempty"`
	Variables     map[string]string `json:"variables,omitempty"`
	Items         []item            `json:"items"`
	SkipKnowledge bool              `json:"skipknowledge"`
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
		return decodeSuggestions(body, query)
	})
}

func decodeSuggestions(body []byte, query string) ([]string, error) {
	var fields []json.RawMessage
	var words []string
	var echoed string
	if !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || len(fields) < 2 ||
		json.Unmarshal(fields[0], &echoed) != nil || echoed != query ||
		json.Unmarshal(fields[1], &words) != nil || words == nil {
		return nil, errors.New("invalid suggestion response")
	}
	return words, nil
}

// Alfred may pass decomposed modern Hangul while Google's results use
// precomposed syllables. Compose only comparison keys; keep displayed text,
// request parameters and existing cache keys in their original form.
func composeHangul(text string) string {
	var result strings.Builder
	var pending rune
	for _, r := range text {
		switch {
		case pending >= 'ᄀ' && pending <= 'ᄒ' && r >= 'ᅡ' && r <= 'ᅵ':
			pending = '가' + ((pending-'ᄀ')*21+r-'ᅡ')*28
			continue
		case pending >= '가' && pending <= '힣' && (pending-'가')%28 == 0 && r >= 'ᆨ' && r <= 'ᇂ':
			pending += r - 0x11A7
			continue
		}
		if pending != 0 {
			result.WriteRune(pending)
		}
		pending = r
	}
	if pending != 0 {
		result.WriteRune(pending)
	}
	return result.String()
}

// Compare the jamo of the final composing syllable as well as literal prefixes.
// A final consonant can move to the next syllable (e.g. 설 → 서루).
func continuesInput(previous, current string) bool {
	if previous == "" {
		return false
	}
	previous, current = strings.ToLower(composeHangul(previous)), strings.ToLower(composeHangul(current))
	if strings.HasPrefix(current, previous) {
		return true
	}
	last, size := utf8.DecodeLastRuneInString(previous)
	rest, ok := strings.CutPrefix(current, previous[:len(previous)-size])
	if !ok || rest == "" {
		return false
	}
	first, size := utf8.DecodeRuneInString(rest)
	next, _ := utf8.DecodeRuneInString(rest[size:])
	return strings.HasPrefix(hangulKeys(first)+hangulKeys(next), hangulKeys(last))
}

func hangulKeys(r rune) string {
	const initials = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ"
	vowels := [...]string{"ㅏ", "ㅐ", "ㅑ", "ㅒ", "ㅓ", "ㅔ", "ㅕ", "ㅖ", "ㅗ", "ㅗㅏ", "ㅗㅐ", "ㅗㅣ", "ㅛ", "ㅜ", "ㅜㅓ", "ㅜㅔ", "ㅜㅣ", "ㅠ", "ㅡ", "ㅡㅣ", "ㅣ"}
	finals := [...]string{"", "ㄱ", "ㄲ", "ㄱㅅ", "ㄴ", "ㄴㅈ", "ㄴㅎ", "ㄷ", "ㄹ", "ㄹㄱ", "ㄹㅁ", "ㄹㅂ", "ㄹㅅ", "ㄹㅌ", "ㄹㅍ", "ㄹㅎ", "ㅁ", "ㅂ", "ㅂㅅ", "ㅅ", "ㅆ", "ㅇ", "ㅈ", "ㅊ", "ㅋ", "ㅌ", "ㅍ", "ㅎ"}
	if r < '가' || r > '힣' {
		return string(r)
	}
	n := r - '가'
	return string([]rune(initials)[n/(21*28)]) + vowels[n/28%21] + finals[n%28]
}

func search(load suggestionLoader, query, pendingQuery, sourceQuery string) feedback {
	out := feedback{Items: []item{}, SkipKnowledge: true}
	if strings.TrimSpace(query) == "" {
		_, _ = load("")
		out.Items = append(out.Items, item{UID: "input", Title: "Google 검색어 입력", Subtitle: "검색어를 입력하세요"})
		return out
	}
	add := func(uid, word string) {
		out.Items = append(out.Items, item{UID: uid, Title: word, Arg: word, Autocomplete: word, Subtitle: "Google에서 검색"})
	}
	add("direct", query)
	// On the first pass, read fresh cached suggestions but never wait for HTTP.
	// Alfred passes the query marker back on the requested rerun.
	if query != pendingQuery {
		load = func(string) ([]byte, error) {
			out.Rerun = 0.1
			out.Variables = map[string]string{"pending_query": query}
			return nil, errors.New("deferred suggestion request")
		}
	}
	words, err := suggestions(load, query)
	listQuery := query
	if err == nil {
		out.Variables = map[string]string{"suggestion_source": query}
	} else {
		previousInput := pendingQuery
		if previousInput == "" {
			previousInput = sourceQuery
		}
		if sourceQuery == "" || !continuesInput(previousInput, query) {
			return out
		}
		// Replay the confirmed list exactly, including its original duplicate
		// filtering. Freshness is checked separately for the new query.
		body, ok := cache.Read(cache.Key("google", sourceQuery), 0)
		if !ok {
			return out
		}
		words, err = decodeSuggestions(body, sourceQuery)
		if err != nil {
			return out
		}
		listQuery = sourceQuery
		out.Variables = map[string]string{"pending_query": query, "suggestion_source": sourceQuery}
	}
	seen := map[string]bool{"": true, composeHangul(listQuery): true}
	for _, word := range words {
		key := composeHangul(word)
		if !seen[key] {
			seen[key] = true
			add("suggestion:"+key, word)
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
		out.Items = append(out.Items, item{UID: updateCommand, Title: "업데이트 확인 및 설치", Arg: updateCommand, Subtitle: "Enter로 최신 GitHub 릴리스를 확인합니다"})
	} else {
		dir := os.Getenv("alfred_workflow_cache")
		auto := dir != "" && current != "" && os.Getenv("automatic_updates") != "0"
		if auto {
			schedule(dir, startCheck)
		}
		out = search(sessionFetch, query, os.Getenv("pending_query"), os.Getenv("suggestion_source"))
		if auto {
			tag, _ := cache.Read(releaseKey, 0)
			if newer(string(tag), current) {
				out.Items = slices.Insert(out.Items, 1, item{UID: updateCommand, Title: "AlfGoogleSuggestPlus " + string(tag) + " 업데이트", Arg: updateCommand, Subtitle: "Enter로 내려받고 Alfred에서 설치"})
			}
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		os.Exit(1)
	}
}
