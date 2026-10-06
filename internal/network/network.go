package network

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultMaxConcurrentDownloads is how many files are fetched at once when the settings do not say
// otherwise. A version has thousands of assets, and fetching them one at a time leaves the
// connection idle between requests.
const DefaultMaxConcurrentDownloads = 16

var (
	downloadLimitMu sync.RWMutex
	downloadLimit   = DefaultMaxConcurrentDownloads
)

// MaxConcurrentDownloads returns how many files a batch downloads at once.
func MaxConcurrentDownloads() int {
	downloadLimitMu.RLock()
	defer downloadLimitMu.RUnlock()
	return downloadLimit
}

// SetMaxConcurrentDownloads changes how many files a batch downloads at once, which the
// download_threads setting does at start-up. A value below one restores the default.
func SetMaxConcurrentDownloads(n int) {
	if n < 1 {
		n = DefaultMaxConcurrentDownloads
	}
	downloadLimitMu.Lock()
	defer downloadLimitMu.Unlock()
	downloadLimit = n
}

type DownloadEntry struct {
	URL      string
	Path     string
	Sha1     string
	FileMode os.FileMode
}

// downloadAttempts is how often one file is fetched before the failure is reported. Downloads now
// run in parallel, which makes a transient 429 or a dropped connection much more likely, and a file
// is cheap to fetch again: it is written from the start and checked against its checksum.
const downloadAttempts = 3

// downloadRetryWait is the pause before the second and third attempt.
var downloadRetryWait = []time.Duration{300 * time.Millisecond, time.Second}

// DownloadFile downloads the specified DownloadEntry and saves it.
//
// All parent directories are created in order to create the file.
//
// A response that only means "not now", or a transfer that ended early, is retried a couple of
// times; the file is rewritten from the start on every attempt, so a retry cannot leave a half
// written file behind.
func DownloadFile(entry DownloadEntry) error {
	var lastErr error
	for attempt := 0; attempt < downloadAttempts; attempt++ {
		err := downloadFileOnce(entry)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < len(downloadRetryWait) && retryable(err) {
			time.Sleep(downloadRetryWait[attempt])
			continue
		}
		return err
	}
	return lastErr
}

// retryable reports whether an error only means the request should be made again.
//
// A rejected checksum counts: it means the body was cut short or corrupted in transit, which a
// second attempt usually fixes.
func retryable(err error) bool {
	var status *HTTPStatusError
	if errors.As(err, &status) {
		return status.StatusCode == http.StatusTooManyRequests || status.StatusCode >= 500
	}
	var checksum *ChecksumError
	if errors.As(err, &checksum) {
		return true
	}
	// A transport failure (reset connection, timeout, DNS hiccup) is worth another try.
	var op *net.OpError
	return errors.As(err, &op)
}

// ChecksumError is returned when a downloaded file does not match the checksum from its metadata.
type ChecksumError struct {
	URL  string
	Want string
	Got  string
}

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("invalid checksum from %q", e.URL)
}

// downloadFileOnce performs a single download attempt.
func downloadFileOnce(entry DownloadEntry) error {
	resp, err := http.Get(entry.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := CheckResponse(resp); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(entry.Path), 0755); err != nil {
		return fmt.Errorf("create directory for file %q: %w", entry.Path, err)
	}
	out, err := os.Create(entry.Path)
	if err != nil {
		return fmt.Errorf("create file %q: %w", entry.Path, err)
	}
	defer out.Close()

	if entry.FileMode != 0 {
		if err := out.Chmod(entry.FileMode); err != nil {
			return fmt.Errorf("set permissions for file %q: %w", entry.Path, err)
		}
	}

	hash := sha1.New()
	tee := io.TeeReader(resp.Body, hash)

	if _, err := io.Copy(out, tee); err != nil {
		return err
	}

	if entry.Sha1 != "" {
		if sum := hex.EncodeToString(hash.Sum(nil)); sum != entry.Sha1 {
			return &ChecksumError{URL: entry.URL, Want: entry.Sha1, Got: sum}
		}
	}

	return nil
}

// StartDownloadEntries downloads every entry with a bounded pool of workers and returns a channel
// with one result per entry.
//
// The results are buffered, so the batch always runs to completion even when the caller stops
// reading early: a caller that gives up on the first error cannot leave workers blocked on a send.
// Results arrive in completion order, not in the order of entries, so a caller that cares about
// order has to sort for itself.
func StartDownloadEntries(entries []DownloadEntry) chan error {
	results := make(chan error, len(entries))
	if len(entries) == 0 {
		close(results)
		return results
	}

	workers := MaxConcurrentDownloads()
	if workers > len(entries) {
		workers = len(entries)
	}

	jobs := make(chan DownloadEntry)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			for entry := range jobs {
				results <- DownloadFile(entry)
			}
		}()
	}

	go func() {
		for _, entry := range entries {
			jobs <- entry
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	return results
}

// DownloadAll downloads every entry and returns the first error, if any.
//
// Unlike StartDownloadEntries it never reports progress, and it waits for the whole batch even after
// a failure, which is what a caller that only needs the files wants.
func DownloadAll(entries []DownloadEntry) error {
	var firstErr error
	for err := range StartDownloadEntries(entries) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// HTTPStatusError is an error type returned when an HTTP response finishes with a status code >= 300 or < 200
type HTTPStatusError struct {
	URL        string
	Method     string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s %s (%d)", e.Method, e.URL, e.StatusCode)
}

// CheckResponse ensures the status code of an HTTP response is successful, returning an HTTPStatusError if not.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return &HTTPStatusError{
			URL:        resp.Request.URL.String(),
			Method:     resp.Request.Method,
			StatusCode: resp.StatusCode,
		}
	}
	return nil
}
