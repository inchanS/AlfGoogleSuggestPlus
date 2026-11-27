package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/inchanS/AlfGoogleSuggestPlus/internal/cache"
)

const (
	updateCommand = "workflow:update"
	releaseAPI    = "https://api.github.com/repos/inchanS/AlfGoogleSuggestPlus/releases/latest"
	assetName     = "AlfGoogleSuggestPlus.alfredworkflow"
	releaseKey    = "__latest_release"
	week          = 7 * 24 * time.Hour
)

func versionParts(s string) ([3]uint64, bool) {
	var result [3]uint64
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != len(result) {
		return result, false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return result, false
		}
		result[i] = n
	}
	return result, true
}

func newer(tag, installed string) bool {
	r, ok := versionParts(tag)
	c, valid := versionParts(installed)
	if ok && valid {
		for i := range r {
			if r[i] != c[i] {
				return r[i] > c[i]
			}
		}
	}
	return false
}

func latest(client *http.Client) (string, error) {
	resp, err := client.Get(releaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Tag        string `json:"tag_name"`
		Draft      bool
		Prerelease bool
		Assets     []struct{ Name string }
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	if _, ok := versionParts(rel.Tag); !ok || !strings.HasPrefix(rel.Tag, "v") || rel.Draft || rel.Prerelease {
		return "", errors.New("지원하지 않는 릴리스")
	}
	for _, a := range rel.Assets {
		if a.Name == assetName {
			return rel.Tag, nil
		}
	}
	return "", errors.New("릴리스에 설치 패키지가 없습니다")
}

func checkRelease(client *http.Client) (string, error) {
	tag, err := latest(client)
	if err == nil {
		_ = cache.Write(releaseKey, []byte(tag))
	}
	return tag, err
}

// Claim a due check before spawning. The lock also covers concurrent keystrokes.
func schedule(dir string, start func() error) {
	if _, fresh := cache.Read(releaseKey, week); fresh {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	stamp, err := os.OpenFile(filepath.Join(dir, ".update-attempt"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return
	}
	defer stamp.Close()
	if syscall.Flock(int(stamp.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	info, err := stamp.Stat()
	if err != nil || (info.Size() > 0 && time.Since(info.ModTime()) < 24*time.Hour) {
		return
	}
	if _, err := stamp.WriteAt([]byte("1"), 0); err != nil {
		return
	}
	if err := start(); err != nil {
		_ = stamp.Truncate(0)
	}
}

func startCheck() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--update", "check")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func install(client *http.Client, current string, open func(string) error) (string, error) {
	tag, err := checkRelease(client)
	if err != nil {
		return "", err
	}
	if _, ok := versionParts(current); !ok {
		return "", errors.New("설치 버전을 확인할 수 없습니다")
	}
	if !newer(tag, current) {
		return "설치할 새 버전이 없습니다.", nil
	}
	resp, err := client.Get("https://github.com/inchanS/AlfGoogleSuggestPlus/releases/download/" + tag + "/" + assetName)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("다운로드 HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "AlfGoogleSuggestPlus-*.alfredworkflow")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, resp.Body)
	err = errors.Join(copyErr, f.Close())
	if err == nil {
		err = validatePackage(f.Name())
	}
	if err == nil {
		err = open(f.Name())
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return tag + " 설치 파일을 열었습니다. Alfred에서 업데이트를 확인하세요.", nil
}

// Reject incomplete downloads and unrelated ZIP files before handing off to Alfred.
func validatePackage(path string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("설치 패키지를 읽을 수 없습니다: %w", err)
	}
	defer archive.Close()
	for _, name := range []string{"info.plist", "googlesuggest"} {
		member, err := archive.Open(name)
		if err != nil {
			return fmt.Errorf("설치 패키지에 %s 파일이 없습니다", name)
		}
		n, readErr := io.Copy(io.Discard, member)
		err = errors.Join(readErr, member.Close())
		if err != nil {
			return fmt.Errorf("설치 패키지의 %s 파일을 읽을 수 없습니다: %w", name, err)
		}
		if n == 0 {
			return fmt.Errorf("설치 패키지의 %s 파일이 비어 있습니다", name)
		}
	}
	return nil
}
