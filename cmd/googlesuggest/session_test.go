package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionReusesWarmConnectionAndExpires(t *testing.T) {
	peers := make(chan string, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers <- r.RemoteAddr
		q := r.URL.Query().Get("q")
		json.NewEncoder(w).Encode([]any{q, []string{q + "!"}})
	}))
	defer upstream.Close()
	dir, err := os.MkdirTemp("/tmp", "suggest-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "session.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan error, 1)
	idle := make(chan struct{}, 3)
	base := upstream.Client().Transport
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		trace := &httptrace.ClientTrace{PutIdleConn: func(err error) {
			if err == nil {
				idle <- struct{}{}
			}
		}}
		return base.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), trace)))
	})}
	go func() { done <- runSession(l, client, upstream.URL, 500*time.Millisecond) }()
	for _, q := range []string{"", "서", "서울"} {
		c, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		var body []byte
		err = json.NewEncoder(c).Encode(q)
		if err == nil {
			err = json.NewDecoder(c).Decode(&body)
		}
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if q == "" {
			// Wait for the warm response to reach the idle pool before simulating typing.
			select {
			case <-idle:
			case <-time.After(time.Second):
				t.Fatal("warm connection was not ready")
			}
		} else {
			want, _ := json.Marshal([]any{q, []string{q + "!"}})
			if string(body) != string(want)+"\n" {
				t.Fatalf("query %q: %s", q, body)
			}
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session did not exit")
	}
	if len(peers) != 3 {
		t.Fatal("wrong number of upstream requests")
	}
	first := <-peers
	if <-peers != first || <-peers != first {
		t.Fatal("connection was not reused")
	}
}
