package logfile_test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/logfile"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, hub *stream.Hub) { logfile.Register(router, provider, hub) })
}

func logFileProvider(paths ...string) *testkit.Provider {
	return &testkit.Provider{LogFilePathFunc: func(index int) (string, error) {
		if index >= len(paths) {
			return "", spec.ErrAPINoLogFileOutput
		}
		return paths[index], nil
	}}
}

func writeLogFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apranik.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func numberedLines(count int) string {
	var lines strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&lines, "line %d\n", index)
	}
	return lines.String()
}

func TestLogFileServesTheWholeFileWithRangeSupport(t *testing.T) {
	path := writeLogFile(t, numberedLines(5))
	server := mount(t, logFileProvider(path))

	status, body, header := server.Do("GET", "/logs/file", "")
	if status != http.StatusOK || body != numberedLines(5) || !strings.HasPrefix(header.Get("Content-Type"), "text/plain") || header.Get("Last-Modified") == "" {
		t.Errorf("whole file = %d %q %v", status, body, header)
	}
	status, body, _ = server.Do("GET", "/logs/file", "", "Range", "bytes=0-5")
	if status != http.StatusPartialContent || body != "line 1" {
		t.Errorf("range = %d %q", status, body)
	}
	_, _, header = server.Do("GET", "/logs/file?download=true", "")
	if header.Get("Content-Disposition") != `attachment; filename="apranik.log"` {
		t.Errorf("Content-Disposition = %q", header.Get("Content-Disposition"))
	}
}

func TestLogFileWithoutAFileOutputOrWithANumberOutOfRangeIs404(t *testing.T) {
	status, body, _ := mount(t, logFileProvider()).Do("GET", "/logs/file", "")
	if status != http.StatusNotFound || testkit.MessageOf(t, body) != "no file log output is configured" {
		t.Errorf("no output: %d %q", status, body)
	}
	status, body, _ = mount(t, logFileProvider(writeLogFile(t, "x\n"))).Do("GET", "/logs/file?output=3", "")
	if status != http.StatusNotFound || !strings.Contains(testkit.MessageOf(t, body), "position 3") {
		t.Errorf("out of range: %d %q", status, body)
	}
}

func TestLogFilePicksTheOutputByPosition(t *testing.T) {
	first, second := writeLogFile(t, "first\n"), writeLogFile(t, "second\n")
	server := mount(t, logFileProvider(first, second))
	if _, body, _ := server.Do("GET", "/logs/file?output=1", ""); body != "second\n" {
		t.Errorf("output 1 = %q", body)
	}
	if _, body, _ := server.Do("GET", "/logs/file", ""); body != "first\n" {
		t.Errorf("default output = %q", body)
	}
}

func TestLogFileTailGivesTheLastCompleteLines(t *testing.T) {
	server := mount(t, logFileProvider(writeLogFile(t, numberedLines(10))))
	_, body, _ := server.Do("GET", "/logs/file?tail=3", "")
	if body != "line 8\nline 9\nline 10\n" {
		t.Errorf("tail=3 = %q", body)
	}
	_, body, _ = server.Do("GET", "/logs/file?tail=500", "")
	if body != numberedLines(10) {
		t.Errorf("tail=500 = %q, want the whole file", body)
	}
}

func TestLogFileTailWithAndWithoutATrailingNewline(t *testing.T) {
	withoutNewline := mount(t, logFileProvider(writeLogFile(t, "a\nb\nc")))
	if _, body, _ := withoutNewline.Do("GET", "/logs/file?tail=2", ""); body != "b\nc\n" {
		t.Errorf("no trailing newline: %q", body)
	}
	blankLines := mount(t, logFileProvider(writeLogFile(t, "a\n\nb\n")))
	if _, body, _ := blankLines.Do("GET", "/logs/file?tail=2", ""); body != "\nb\n" {
		t.Errorf("a blank line counts: %q", body)
	}
}

func TestLogFileTailReadsBackwardsAcrossBlocksAndVeryLongLines(t *testing.T) {
	long := strings.Repeat("x", 200000)
	var content strings.Builder
	for index := 1; index <= 3000; index++ {
		fmt.Fprintf(&content, "padding line %d %s\n", index, strings.Repeat("p", 40))
	}
	content.WriteString("before the long line\n" + long + "\nafter the long line\n")
	server := mount(t, logFileProvider(writeLogFile(t, content.String())))

	_, body, _ := server.Do("GET", "/logs/file?tail=3", "")
	if body != "before the long line\n"+long+"\nafter the long line\n" {
		t.Errorf("tail across a line longer than a block: %d bytes, want the three lines", len(body))
	}
	_, body, _ = server.Do("GET", "/logs/file?tail=2000", "")
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) != 2000 || lines[len(lines)-1] != "after the long line" || !strings.HasPrefix(lines[0], "padding line ") {
		t.Errorf("tail=2000 gave %d lines starting %q", len(lines), lines[0][:min(20, len(lines[0]))])
	}
}

func TestLogFileContainsFiltersBeforeTail(t *testing.T) {
	var content strings.Builder
	for index := 1; index <= 100; index++ {
		kind := "ok"
		if index%10 == 0 {
			kind = "refused"
		}
		fmt.Fprintf(&content, "%d %s\n", index, kind)
	}
	server := mount(t, logFileProvider(writeLogFile(t, content.String())))

	_, body, _ := server.Do("GET", "/logs/file?contains=refused&tail=3", "")
	if body != "80 refused\n90 refused\n100 refused\n" {
		t.Errorf("contains+tail = %q, want the last 3 matching lines", body)
	}
	_, body, _ = server.Do("GET", "/logs/file?contains=refused", "")
	if strings.Count(body, "\n") != 10 || !strings.HasPrefix(body, "10 refused\n") {
		t.Errorf("contains = %q", body)
	}
	_, body, _ = server.Do("GET", "/logs/file?contains=REFUSED", "")
	if body != "" {
		t.Errorf("contains is case-sensitive, got %q", body)
	}
	_, body, _ = server.Do("GET", "/logs/file?contains=1+r", "")
	if body != "" {
		t.Errorf("contains is a plain substring, not a pattern: %q", body)
	}
}

func TestLogFileContainsWithoutNewlineAtTheEndKeepsTheLastLine(t *testing.T) {
	server := mount(t, logFileProvider(writeLogFile(t, "keep a\ndrop\nkeep b")))
	if _, body, _ := server.Do("GET", "/logs/file?contains=keep", ""); body != "keep a\nkeep b\n" {
		t.Errorf("body = %q", body)
	}
}

func TestLogFileRefusesBadParametersAndIgnoresAnyPathFromTheClient(t *testing.T) {
	server := mount(t, logFileProvider(writeLogFile(t, "inside\n")))
	for _, query := range []string{"tail=0", "tail=-1", "tail=x", "tail=100001", "output=-1", "output=x", "follow=maybe", "download=2"} {
		if status, _, _ := server.Do("GET", "/logs/file?"+query, ""); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", query, status)
		}
	}
	_, body, _ := server.Do("GET", "/logs/file?path=/etc/passwd&file=/etc/passwd", "")
	if body != "inside\n" {
		t.Errorf("a path in the query changed what was served: %q", body)
	}
}

func TestLogFileThatCannotBeOpenedIs503(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.log")
	status, body, _ := mount(t, logFileProvider(missing)).Do("GET", "/logs/file", "")
	if status != http.StatusServiceUnavailable || !strings.Contains(testkit.MessageOf(t, body), "cannot be opened") {
		t.Errorf("%d %q", status, body)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := io.WriteString(file, text); err != nil {
		t.Fatal(err)
	}
}

func readFollowedLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("reading a followed line: %v", got.err)
		}
		return got.line
	case <-time.After(5 * time.Second):
		t.Fatal("no line arrived while following")
		return ""
	}
}

func TestLogFileFollowSendsWhatIsAppendedAndOnlyCompleteLines(t *testing.T) {
	path := writeLogFile(t, "old 1\nold 2\n")
	server := mount(t, logFileProvider(path))
	_, reader := server.OpenLines("/logs/file?follow=true&tail=1")

	if line := readFollowedLine(t, reader); line != "old 2\n" {
		t.Fatalf("first line %q, want the tail", line)
	}
	appendTo(t, path, "new 1\nnew ")
	if line := readFollowedLine(t, reader); line != "new 1\n" {
		t.Fatalf("appended line %q", line)
	}
	appendTo(t, path, "2\n")
	if line := readFollowedLine(t, reader); line != "new 2\n" {
		t.Errorf("the half-written line arrived as %q, want it whole once finished", line)
	}
}

func TestLogFileFollowWithoutTailStartsWithTheWholeFileAndFiltersWhatFollows(t *testing.T) {
	path := writeLogFile(t, "warn a\ninfo b\n")
	server := mount(t, logFileProvider(path))
	_, reader := server.OpenLines("/logs/file?follow=true&contains=warn")

	if line := readFollowedLine(t, reader); line != "warn a\n" {
		t.Fatalf("first %q", line)
	}
	appendTo(t, path, "info c\nwarn d\n")
	if line := readFollowedLine(t, reader); line != "warn d\n" {
		t.Errorf("followed %q, want only the line containing warn", line)
	}
}

func TestLogFileFollowSurvivesTruncationAndRotation(t *testing.T) {
	path := writeLogFile(t, "one\ntwo\nthree\n")
	server := mount(t, logFileProvider(path))
	_, reader := server.OpenLines("/logs/file?follow=true&tail=1")
	if line := readFollowedLine(t, reader); line != "three\n" {
		t.Fatalf("first %q", line)
	}

	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "after truncation\n")
	if line := readFollowedLine(t, reader); line != "after truncation\n" {
		t.Errorf("after a truncation: %q", line)
	}

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "in the new file\n")
	if line := readFollowedLine(t, reader); line != "in the new file\n" {
		t.Errorf("after a rotation: %q", line)
	}
}

func TestLogFileFollowEndsWhenTheServerCloses(t *testing.T) {
	path := writeLogFile(t, "a\n")
	server := mount(t, logFileProvider(path))
	_, reader := server.OpenLines("/logs/file?follow=true")
	readFollowedLine(t, reader)

	closed := make(chan struct{})
	go func() { server.Hub.Close(); server.Hub.Wait(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return while a log file was followed")
	}
}
