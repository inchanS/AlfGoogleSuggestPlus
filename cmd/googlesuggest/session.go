package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const requestTimeout = 1500 * time.Millisecond
const sessionIdle = 10 * time.Second

// macOS limits Unix socket paths to 103 bytes. Alfred's cache path can be longer.
func sessionDir() string {
	key := sha256.Sum256([]byte(os.Getenv("alfred_workflow_cache")))
	return fmt.Sprintf("/tmp/alfgooglesuggest-%d-%x", os.Getuid(), key[:8])
}

func sessionFetch(query string) ([]byte, error) {
	dir := sessionDir()
	socket := filepath.Join(dir, "session.sock")
	var conn net.Conn
	err := privateSessionDir(dir)
	if err == nil {
		conn, err = net.Dial("unix", socket)
		if err != nil {
			conn, err = startSession(socket)
		}
	}
	if err == nil {
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(requestTimeout + 250*time.Millisecond))
		var body []byte
		if json.NewEncoder(conn).Encode(query) == nil && json.NewDecoder(conn).Decode(&body) == nil {
			// A nil body acknowledges warmup or an upstream failure.
			if query != "" && body == nil {
				return nil, errors.New("no suggestion response")
			}
			return body, nil
		}
	}
	if query == "" {
		return nil, nil
	}
	return fetch(&http.Client{Timeout: requestTimeout}, endpoint, query)
}

func startSession(socket string) (net.Conn, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "--session", "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_ = cmd.Process.Release()
	// Simultaneous launches elect one listener under the file lock.
	until := time.Now().Add(150 * time.Millisecond)
	for {
		conn, err := net.Dial("unix", socket)
		if err == nil || time.Now().After(until) {
			return conn, err
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func privateSessionDir(dir string) error {
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("invalid session directory")
	}
	return nil
}

func serveSession() error {
	dir := sessionDir()
	if err := privateSessionDir(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	socket := filepath.Join(dir, "session.sock")
	// A killed worker can leave a stale socket. Only the lock owner may replace it.
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	client := &http.Client{Timeout: requestTimeout}
	defer client.CloseIdleConnections()
	return runSession(listener, client, endpoint, sessionIdle)
}

func runSession(listener *net.UnixListener, client *http.Client, url string, idle time.Duration) error {
	var warm sync.Once
	var active sync.WaitGroup
	defer active.Wait()
	for {
		listener.SetDeadline(time.Now().Add(idle))
		conn, err := listener.AcceptUnix()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil
			}
			return err
		}
		active.Add(1)
		go func() {
			defer active.Done()
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(requestTimeout + 250*time.Millisecond))
			var query string
			if json.NewDecoder(conn).Decode(&query) != nil {
				return
			}
			if query == "" {
				_ = json.NewEncoder(conn).Encode(nil)
				warm.Do(func() { _, _ = fetch(client, url, "") })
				return
			}
			body, _ := fetch(client, url, query)
			_ = json.NewEncoder(conn).Encode(body)
		}()
	}
}
