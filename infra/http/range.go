package http

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ServeRange writes a single byte range from r to w, setting the
// appropriate 206 / Content-Range / Content-Length headers. Falls back
// to a full-body 200 response when no Range header is present or when
// the header is malformed.
//
// rsCloser may be a *bytes.Reader, *os.File, or any other ReadSeeker.
// total is the full content length (callers usually have it cheaply
// from os.Stat).
//
// Designed as the building block for file / file-server / static
// route types when range support is desired without bringing in
// http.ServeContent (which we already use elsewhere — this helper is
// for handlers that already manage their own io.ReadSeeker pipeline,
// e.g. plugin-served binary data).
func ServeRange(w http.ResponseWriter, r *http.Request, body io.ReadSeeker, total int64, contentType string) error {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Accept-Ranges", "bytes")

	rh := r.Header.Get("Range")
	if rh == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		w.WriteHeader(http.StatusOK)
		_, err := io.Copy(w, body)
		return err
	}

	start, end, err := parseSingleRange(rh, total)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return err
	}
	if _, err := body.Seek(start, io.SeekStart); err != nil {
		http.Error(w, "seek error", http.StatusInternalServerError)
		return err
	}
	length := end - start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	_, err = io.CopyN(w, body, length)
	return err
}

// parseSingleRange handles the common forms:
//
//	bytes=START-END
//	bytes=START-
//	bytes=-SUFFIX (last SUFFIX bytes)
//
// Multi-range requests aren't supported — they're rare and force
// multipart/byteranges encoding. Callers fall back to full-body
// delivery in that case (parseSingleRange returns an error).
func parseSingleRange(header string, total int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errors.New("only `bytes=` ranges supported")
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, errors.New("multi-range not supported")
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, errors.New("malformed range")
	}
	startStr := spec[:dash]
	endStr := spec[dash+1:]

	if startStr == "" {
		// Suffix: last N bytes.
		suffix, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, errors.New("bad suffix length")
		}
		if suffix > total {
			suffix = total
		}
		return total - suffix, total - 1, nil
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, errors.New("bad start")
	}
	if start >= total {
		return 0, 0, fmt.Errorf("start %d beyond total %d", start, total)
	}
	end := total - 1
	if endStr != "" {
		v, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || v < start {
			return 0, 0, errors.New("bad end")
		}
		end = v
	}
	if end >= total {
		end = total - 1
	}
	return start, end, nil
}
