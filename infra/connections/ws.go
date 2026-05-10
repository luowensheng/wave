package connections

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// safeWriter serializes all writes to the underlying buffered writer so
// the broker pump goroutine and the read-loop goroutine (which echoes
// pings as pongs and acks closes) don't race on bufio internals.
type safeWriter struct {
	mu  sync.Mutex
	brw *bufio.ReadWriter
}

func (s *safeWriter) write(op byte, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFrame(s.brw, op, payload); err != nil {
		return err
	}
	return s.brw.Flush()
}

func (s *safeWriter) close(code uint16, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeCloseFrame(s.brw, code, reason); err != nil {
		return err
	}
	return s.brw.Flush()
}

// Minimal RFC 6455 WebSocket server.
//
// Scope: server-to-client text frames + server-initiated pings + handling
// of client pings/pongs/close. We don't ship a writeable WS API to user
// code — connections are a server→client broadcast channel, so reads
// are mostly status/keepalive.
//
// Why hand-rolled: keeps the project's zero-runtime-dep story intact
// (no gorilla/websocket, no x/net/websocket). The framing surface we
// need is small enough to be auditable here.

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11" // RFC 6455 §1.3

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// ServeWS upgrades the request to WebSocket and pumps broker events as
// text frames until the client disconnects, sends close, or the
// request context is cancelled. Heartbeat pings keep middleboxes happy.
//
// Subscriber count + slow-client policy reuses the same Broker, so
// metrics work uniformly across SSE and WS.
func ServeWS(w http.ResponseWriter, r *http.Request, broker *Broker) {
	conn, brw, err := upgradeWS(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	sw := &safeWriter{brw: brw}

	id := strconv.FormatInt(subscriberSeq.Add(1), 10) + "-ws"
	ch, cancel, ok := broker.Subscribe(id)
	if !ok {
		_ = sw.close(1013, "max clients") // 1013 = try again later
		return
	}
	defer cancel()

	pingEvery := pingDuration(broker.cfg)
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()

	// Reader goroutine: handles client pings/close. Send notifications
	// back through `clientErr` so the writer can quit cleanly. Shares a
	// safeWriter so writes don't race the broker-pump writes below.
	clientErr := make(chan error, 1)
	go readLoop(brw.Reader, sw, clientErr)

	for {
		select {
		case <-r.Context().Done():
			_ = sw.close(1001, "going away")
			return
		case <-clientErr:
			return
		case <-ping.C:
			if err := sw.write(opPing, nil); err != nil {
				return
			}
		case event, ok := <-ch:
			if !ok {
				_ = sw.close(1000, "")
				return
			}
			// Strip the SSE wire format ("event: ...\ndata: ...\n\n") down to
			// just the JSON `data:` line for WS clients. Keeps the publisher
			// transport-agnostic.
			if err := sw.write(opText, stripSSEDataLine(event)); err != nil {
				return
			}
		}
	}
}

// pingDuration picks the WS ping interval, falling back to keep_alive_interval
// or 30s default.
func pingDuration(cfg *ConnectionConfig) time.Duration {
	if d, err := time.ParseDuration(cfg.PingInterval); err == nil && d > 0 {
		return d
	}
	if d, err := time.ParseDuration(cfg.KeepAliveInterval); err == nil && d > 0 {
		return d
	}
	return 30 * time.Second
}

// stripSSEDataLine pulls out the JSON payload from a pre-formatted SSE
// frame. Falls back to the raw bytes if it doesn't look like SSE.
func stripSSEDataLine(b []byte) []byte {
	const dataPrefix = "data: "
	for {
		nl := indexByte(b, '\n')
		if nl < 0 {
			return b
		}
		line := b[:nl]
		if hasPrefix(line, dataPrefix) {
			return line[len(dataPrefix):]
		}
		b = b[nl+1:]
		if len(b) == 0 {
			return b
		}
	}
}

func hasPrefix(b []byte, p string) bool {
	if len(b) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if b[i] != p[i] {
			return false
		}
	}
	return true
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// upgradeWS performs the RFC 6455 server handshake. Returns the hijacked
// underlying conn + a buffered writer + the original buffered reader.
type wsConn struct {
	rwc io.ReadWriteCloser
}

func (c *wsConn) Close() error { return c.rwc.Close() }

func upgradeWS(w http.ResponseWriter, r *http.Request) (*wsConn, *bufio.ReadWriter, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		!headerContainsToken(r.Header.Get("Connection"), "Upgrade") {
		return nil, nil, errors.New("not a websocket upgrade request")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, nil, errors.New("unsupported websocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, nil, errors.New("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijack: %w", err)
	}

	accept := wsAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := brw.WriteString(resp); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := brw.Flush(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return &wsConn{rwc: conn}, brw, nil
}

// wsAcceptKey is base64(sha1(key + GUID)) per RFC 6455.
func wsAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContainsToken(h, token string) bool {
	for _, part := range strings.Split(h, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// writeFrame writes a single unfragmented frame. Server frames are
// never masked (RFC 6455 §5.1).
func writeFrame(w io.Writer, opcode byte, payload []byte) error {
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|opcode) // FIN=1
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeCloseFrame(w io.Writer, code uint16, reason string) error {
	body := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(body[:2], code)
	copy(body[2:], reason)
	return writeFrame(w, opClose, body)
}

// readLoop reads incoming frames, replies to pings, surfaces close.
// We don't deliver text frames anywhere — connections are server-push.
// All writes go through safeWriter so they don't race the broker pump.
func readLoop(br *bufio.Reader, sw *safeWriter, errCh chan<- error) {
	defer func() { _ = recover() }()
	for {
		op, payload, err := readFrame(br)
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}
		switch op {
		case opPing:
			_ = sw.write(opPong, payload)
		case opClose:
			_ = sw.close(1000, "")
			select {
			case errCh <- io.EOF:
			default:
			}
			return
		case opPong, opText, opBinary, opContinuation:
			// ignore
		}
	}
}

func readFrame(br *bufio.Reader) (byte, []byte, error) {
	b1, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	op := b1 & 0x0F

	b2, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	masked := (b2 & 0x80) != 0
	length := uint64(b2 & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > 1<<20 { // 1 MiB control-frame cap
		return 0, nil, fmt.Errorf("frame too large: %d", length)
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(br, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return op, payload, nil
}

// ── handler dispatch ──────────────────────────────────────────────────────

// connectionTypeKind picks SSE/WS for `auto` based on request headers.
func connectionTypeKind(cfg *ConnectionConfig, r *http.Request) string {
	switch cfg.Type {
	case "ws":
		return "ws"
	case "auto":
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			return "ws"
		}
		return "sse"
	default:
		return "sse"
	}
}

// Serve dispatches to ServeSSE or ServeWS based on cfg.Type and request
// headers. This is the entry point the route auto-registration uses so
// `type: ws` and `type: auto` work without changing the subscribe-route
// handler in orchestrator/server/subscribe.go.
func Serve(w http.ResponseWriter, r *http.Request, broker *Broker) {
	switch connectionTypeKind(broker.cfg, r) {
	case "ws":
		ServeWS(w, r, broker)
	default:
		ServeSSE(w, r, broker)
	}
}

// shut up the unused import linter for builds without ws (we always build
// it in this repo, but this helps if someone strips the file).
var _ = atomic.Int64{}
