// Package logfile serves /logs/file: the log file as the core wrote it, for a
// person or a script. This is Apranik's own endpoint, not Clash's. The path comes
// only from the configuration (the provider), never from the request.
package logfile

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func Register(router *web.Router, provider spec.APICoreProvider, hub *stream.Hub) {
	handler := handler{provider: provider, hub: hub}
	router.Handle("/logs/file", web.Methods{"GET": handler.serve})
}

type handler struct {
	provider spec.APICoreProvider
	hub      *stream.Hub
}

const (
	// PROVISIONAL limits.
	maxTail          = 100000
	blockBytes       = 64 << 10
	followChunkBytes = 1 << 20
	pollInterval     = 250 * time.Millisecond
	writeTimeout     = 2 * time.Second
	bufferBytes      = 32 << 10
)

type parameters struct {
	output   int
	tail     int // zero: not asked
	contains []byte
	follow   bool
	download bool
}

func parametersOf(query url.Values) (parameters, error) {
	request := parameters{contains: []byte(query.Get("contains"))}
	var err error
	if text := query.Get("output"); text != "" {
		if request.output, err = strconv.Atoi(text); err != nil || request.output < 0 {
			return request, web.BadParameter("output is the position of a file output in the configuration, from 0, not %q", text)
		}
	}
	if text := query.Get("tail"); text != "" {
		if request.tail, err = strconv.Atoi(text); err != nil || request.tail < 1 || request.tail > maxTail {
			return request, web.BadParameter("tail is a number of lines from 1 to %d, not %q", maxTail, text)
		}
	}
	for name, setting := range map[string]*bool{"follow": &request.follow, "download": &request.download} {
		if text := query.Get(name); text != "" {
			if *setting, err = strconv.ParseBool(text); err != nil {
				return request, web.BadParameter("%s is true or false, not %q", name, text)
			}
		}
	}
	return request, nil
}

func (r parameters) matches(line []byte) bool {
	return len(r.contains) == 0 || bytes.Contains(line, r.contains)
}

func (h handler) serve(w http.ResponseWriter, r *http.Request) {
	request, err := parametersOf(r.URL.Query())
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "%v", err)
		return
	}
	path, err := h.provider.LogFilePath(request.output)
	if err != nil {
		if errors.Is(err, spec.ErrAPINoLogFileOutput) && request.output > 0 {
			web.WriteError(w, http.StatusNotFound, "the configuration has no file log output at position %d", request.output)
			return
		}
		web.WriteProviderError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		web.WriteError(w, http.StatusServiceUnavailable, "the log file cannot be opened: %v", err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		web.WriteError(w, http.StatusServiceUnavailable, "the log file cannot be read: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if request.download {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	}
	if request.tail == 0 && len(request.contains) == 0 && !request.follow {
		// Range, If-Modified-Since and Last-Modified come with it.
		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
		return
	}

	ctx := r.Context()
	if request.follow {
		endTracking, ok := h.hub.Track()
		if !ok {
			web.WriteError(w, http.StatusServiceUnavailable, "the API is closing")
			return
		}
		defer endTracking()
		var cancel context.CancelFunc
		ctx, cancel = h.hub.Bind(ctx)
		defer cancel()
	}
	write(ctx, w, path, file, info, request)
}

// deadlineWriter bounds each write, so a client that stopped reading does not
// hold the handler.
type deadlineWriter struct {
	controller *http.ResponseController
	writer     http.ResponseWriter
}

func (d deadlineWriter) Write(data []byte) (int, error) {
	d.controller.SetWriteDeadline(time.Now().Add(writeTimeout))
	return d.writer.Write(data)
}

func write(ctx context.Context, w http.ResponseWriter, path string, file *os.File, info os.FileInfo, request parameters) {
	controller := http.NewResponseController(w)
	output := bufio.NewWriterSize(deadlineWriter{controller: controller, writer: w}, bufferBytes)
	flush := func() error {
		if err := output.Flush(); err != nil {
			return err
		}
		controller.SetWriteDeadline(time.Now().Add(writeTimeout))
		return controller.Flush()
	}
	emit := func(line []byte) error {
		if _, err := output.Write(line); err != nil {
			return err
		}
		return output.WriteByte('\n')
	}

	end := info.Size()
	if request.follow {
		// A line still being written is not sent until it is complete.
		completeEnd, err := endOfLastCompleteLine(file, end)
		if err != nil {
			return
		}
		end = completeEnd
	}
	// Taken before anything is sent: once the client has the first lines it may
	// rewrite the file at once, and the anchor must describe what was sent.
	anchor := bytesBefore(file, end)

	if request.tail > 0 {
		lines, err := lastMatchingLines(file, end, request.tail, request.matches)
		if err != nil {
			return
		}
		for _, line := range lines {
			if emit(line) != nil {
				return
			}
		}
	} else {
		// The region ends at a newline when following, so only a plain read can
		// end in an unfinished line, which is a line of the file as it is now.
		_, unfinished, err := forwardMatchingLines(io.NewSectionReader(file, 0, end), request, emit)
		if err != nil {
			return
		}
		if len(unfinished) > 0 && request.matches(unfinished) && emit(unfinished) != nil {
			return
		}
	}
	if flush() != nil || !request.follow {
		return
	}
	followAppended(ctx, path, file, info, end, anchor, request, emit, flush)
}

// endOfLastCompleteLine is the position just after the last newline before end,
// or 0 when there is none.
func endOfLastCompleteLine(file *os.File, end int64) (int64, error) {
	block := make([]byte, blockBytes)
	for position := end; position > 0; {
		size := min(int64(len(block)), position)
		position -= size
		if _, err := file.ReadAt(block[:size], position); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if index := bytes.LastIndexByte(block[:size], '\n'); index >= 0 {
			return position + int64(index) + 1, nil
		}
	}
	return 0, nil
}

// lastMatchingLines returns up to limit of the matching lines before end, oldest
// first, reading backwards in blocks so a big file is never read whole. A last
// line without a newline counts as a line.
func lastMatchingLines(file *os.File, end int64, limit int, matches func([]byte) bool) ([][]byte, error) {
	if end > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, end-1); err != nil {
			return nil, err
		}
		if last[0] == '\n' {
			end--
		}
	}

	var newestFirst [][]byte
	consider := func(line []byte) {
		if matches(line) {
			newestFirst = append(newestFirst, bytes.Clone(line))
		}
	}
	var carry []byte
	for position := end; position > 0 && len(newestFirst) < limit; {
		size := min(int64(blockBytes), position)
		position -= size
		block := make([]byte, size, int(size)+len(carry))
		if _, err := file.ReadAt(block, position); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		data := append(block, carry...)
		carry = nil
		for len(newestFirst) < limit {
			index := bytes.LastIndexByte(data, '\n')
			if index < 0 {
				if position == 0 {
					consider(data)
				} else {
					carry = data
				}
				break
			}
			consider(data[index+1:])
			data = data[:index]
		}
	}
	if end == 0 {
		return nil, nil
	}

	lines := make([][]byte, len(newestFirst))
	for index, line := range newestFirst {
		lines[len(lines)-1-index] = line
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

// forwardMatchingLines sends every matching complete line of the reader and
// returns where the last complete line ended and the unfinished line after it.
func forwardMatchingLines(source io.Reader, request parameters, emit func([]byte) error) (offset int64, pending []byte, err error) {
	reader := bufio.NewReaderSize(source, blockBytes)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == nil {
			offset += int64(len(line))
			if body := line[:len(line)-1]; request.matches(body) {
				if err := emit(body); err != nil {
					return offset, nil, err
				}
			}
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return offset, line, nil
		}
		return offset, nil, readErr
	}
}

// followAppended sends what is appended to the file until the request ends. If
// the file shrinks, is rewritten or is replaced (truncation, rotation) it starts
// again from the beginning of the new one. A file that was truncated and grew
// past the old offset again between two polls is told by the bytes just before
// the offset, which are no longer what was read.
func followAppended(ctx context.Context, path string, file *os.File, info os.FileInfo, offset int64, anchor []byte, request parameters, emit func([]byte) error, flush func() error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var pending []byte
	current, currentInfo := file, info
	opened := false
	defer func() {
		if opened {
			current.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		onDisk, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !os.SameFile(currentInfo, onDisk) {
			replacement, err := os.Open(path)
			if err != nil {
				continue
			}
			if opened {
				current.Close()
			}
			current, opened = replacement, true
			if currentInfo, err = replacement.Stat(); err != nil {
				continue
			}
			offset, pending, anchor = 0, nil, nil
			onDisk = currentInfo
		}
		if onDisk.Size() < offset || !bytes.Equal(bytesBefore(current, offset), anchor) {
			offset, pending, anchor = 0, nil, nil
		}
		if onDisk.Size() == offset {
			continue
		}

		chunk := make([]byte, min(onDisk.Size()-offset, followChunkBytes))
		read, err := current.ReadAt(chunk, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			continue
		}
		offset += int64(read)
		anchor = bytesBefore(current, offset)
		data := append(pending, chunk[:read]...)
		pending = nil
		for {
			index := bytes.IndexByte(data, '\n')
			if index < 0 {
				pending = bytes.Clone(data)
				break
			}
			if line := data[:index]; request.matches(line) && emit(line) != nil {
				return
			}
			data = data[index+1:]
		}
		if flush() != nil {
			return
		}
	}
}

// followAnchorBytes is how much of the file just before the offset is kept to
// notice that the file was rewritten.
const followAnchorBytes = 64

// bytesBefore is the last bytes of the file before offset, nil when there are
// none or they cannot be read.
func bytesBefore(file *os.File, offset int64) []byte {
	size := min(offset, followAnchorBytes)
	if size <= 0 {
		return nil
	}
	anchor := make([]byte, size)
	if _, err := file.ReadAt(anchor, offset-size); err != nil {
		return nil
	}
	return anchor
}
