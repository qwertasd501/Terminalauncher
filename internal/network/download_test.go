package network_test

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/telecter/cmd-launcher/internal/network"
)

// withDownloadLimit runs the test body with a download thread count of limit, restoring the
// previous value afterwards.
func withDownloadLimit(t *testing.T, limit int) {
	t.Helper()
	previous := network.MaxConcurrentDownloads()
	network.SetMaxConcurrentDownloads(limit)
	t.Cleanup(func() { network.SetMaxConcurrentDownloads(previous) })
}

// TestMaxConcurrentDownloads checks the accessor around the download thread count: a count below one
// falls back to the built-in default, which is what an unset settings file stores.
func TestMaxConcurrentDownloads(t *testing.T) {
	previous := network.MaxConcurrentDownloads()
	t.Cleanup(func() { network.SetMaxConcurrentDownloads(previous) })

	network.SetMaxConcurrentDownloads(3)
	if got := network.MaxConcurrentDownloads(); got != 3 {
		t.Errorf("MaxConcurrentDownloads() = %d, want 3", got)
	}
	for _, value := range []int{0, -5} {
		network.SetMaxConcurrentDownloads(value)
		if got := network.MaxConcurrentDownloads(); got != network.DefaultMaxConcurrentDownloads {
			t.Errorf("SetMaxConcurrentDownloads(%d) left %d, want the default %d", value, got, network.DefaultMaxConcurrentDownloads)
		}
	}
}

// TestStartDownloadEntriesDownloadsEveryFile checks that a parallel batch writes every file, and
// never runs more downloads at once than the thread count allows.
func TestStartDownloadEntriesDownloadsEveryFile(t *testing.T) {
	const files = 24
	const limit = 3
	withDownloadLimit(t, limit)

	var running, peak int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&running, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if current <= old || atomic.CompareAndSwapInt32(&peak, old, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		fmt.Fprint(w, strings.TrimPrefix(r.URL.Path, "/"))
	}))
	defer server.Close()

	dir := t.TempDir()
	entries := make([]network.DownloadEntry, files)
	want := make(map[string]string, files)
	for i := range entries {
		body := "file-" + strconv.Itoa(i)
		sum := sha1.Sum([]byte(body))
		entries[i] = network.DownloadEntry{
			URL:  server.URL + "/" + body,
			Path: filepath.Join(dir, body),
			Sha1: hex.EncodeToString(sum[:]),
		}
		want[filepath.Join(dir, body)] = body
	}

	if err := network.DownloadAll(entries); err != nil {
		t.Fatalf("DownloadAll: %v", err)
	}

	for path, body := range want {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("downloaded file %q is missing: %v", path, err)
		}
		if string(data) != body {
			t.Errorf("%s holds %q, want %q", filepath.Base(path), data, body)
		}
	}

	if peak > limit {
		t.Errorf("ran %d downloads at once, the limit was %d", peak, limit)
	}
	if peak < 2 {
		t.Errorf("downloads never overlapped (peak %d), the pool is not running in parallel", peak)
	}
}

// TestDownloadFileRetriesRateLimit checks that being told to slow down is retried rather than
// reported: a burst of parallel downloads makes that answer likely.
func TestDownloadFileRetriesRateLimit(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "retried.bin")
	err := network.DownloadFile(network.DownloadEntry{URL: server.URL, Path: path})
	if err != nil {
		t.Fatalf("a rate limited download was not retried: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("server saw %d attempts, want 2", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "payload" {
		t.Fatalf("retried download left %q (%v)", data, err)
	}
}

// TestDownloadFileReportsPermanentFailure checks that an error that will not go away is reported as
// an HTTPStatusError instead of being retried until the attempts run out silently.
func TestDownloadFileReportsPermanentFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := network.DownloadFile(network.DownloadEntry{URL: server.URL, Path: filepath.Join(t.TempDir(), "missing.bin")})
	var status *network.HTTPStatusError
	if !errors.As(err, &status) {
		t.Fatalf("error is %v, want an HTTPStatusError", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("server saw %d attempts, a 404 should not be retried", got)
	}
}

// TestStartDownloadEntriesCompletesAfterEarlyAbandonment checks that a caller which stops reading on
// the first error cannot leave the batch blocked: the results channel is buffered and closes.
func TestStartDownloadEntriesCompletesAfterEarlyAbandonment(t *testing.T) {
	withDownloadLimit(t, 2)

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, "body")
	}))
	defer server.Close()

	dir := t.TempDir()
	entries := make([]network.DownloadEntry, 8)
	for i := range entries {
		entries[i] = network.DownloadEntry{
			URL:  server.URL + "/" + strconv.Itoa(i),
			Path: filepath.Join(dir, strconv.Itoa(i)),
		}
	}

	// Read a single result and walk away, the way a caller that aborts on the first error does.
	results := network.StartDownloadEntries(entries)
	<-results

	// The batch must still finish; every entry was fetched exactly once.
	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&attempts) < int32(len(entries)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&attempts); got != int32(len(entries)) {
		t.Fatalf("server saw %d requests, want %d: the batch stopped early", got, len(entries))
	}
	for i := range entries {
		if _, err := os.Stat(entries[i].Path); err != nil {
			t.Errorf("file %d was not downloaded: %v", i, err)
		}
	}
}

// TestStartDownloadEntriesEmpty checks that an empty batch closes its channel instead of blocking.
func TestStartDownloadEntriesEmpty(t *testing.T) {
	var mu sync.Mutex
	count := 0
	for range network.StartDownloadEntries(nil) {
		mu.Lock()
		count++
		mu.Unlock()
	}
	if count != 0 {
		t.Errorf("empty batch reported %d results", count)
	}
}
