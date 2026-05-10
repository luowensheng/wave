package connections

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWSAcceptKey(t *testing.T) {
	// RFC 6455 example: key "dGhlIHNhbXBsZSBub25jZQ==" → "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := wsAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	if got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("got %q", got)
	}
}

func TestStripSSEDataLine(t *testing.T) {
	in := []byte("event: payment\ndata: {\"id\":1}\n\n")
	out := stripSSEDataLine(in)
	if string(out) != `{"id":1}` {
		t.Errorf("got %q", out)
	}
}

// makeClientFrame builds a single masked client→server text frame.
func makeClientFrame(op byte, payload []byte) []byte {
	mask := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	hdr := []byte{0x80 | op, 0x80 | byte(len(payload))}
	hdr = append(hdr, mask[:]...)
	return append(hdr, masked...)
}

// readWSFrame parses a server→client (unmasked) frame from r.
func readWSFrame(t *testing.T, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	op, payload, err := readFrame(br)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	return op, payload
}

func TestServeWSEndToEnd(t *testing.T) {
	broker := NewBroker(&ConnectionConfig{
		Type:          "ws",
		SubscribePath: "/ws",
		BufferSize:    8,
		PingInterval:  "100ms",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWS(w, r, broker)
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	c, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	br := bufio.NewReader(c)

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	expectedAccept := wsAcceptKey(key)
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	// Parse handshake response.
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if statusLine != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("status: %q", statusLine)
	}
	gotAccept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
		if len(line) > 22 && line[:22] == "Sec-WebSocket-Accept: " {
			gotAccept = line[22 : len(line)-2]
		}
	}
	if gotAccept != expectedAccept {
		t.Fatalf("accept mismatch: got %q want %q", gotAccept, expectedAccept)
	}

	// Server should send no frames yet. Now publish via the broker and
	// verify the client receives a text frame with the SSE data line stripped.
	go func() {
		time.Sleep(20 * time.Millisecond)
		broker.Publish([]byte("event: x\ndata: {\"hi\":1}\n\n"))
	}()

	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	op, payload := readWSFrame(t, br)
	// First frame might be a ping due to short PingInterval — skip it.
	if op == opPing {
		op, payload = readWSFrame(t, br)
	}
	if op != opText {
		t.Fatalf("op = %d", op)
	}
	if string(payload) != `{"hi":1}` {
		t.Errorf("payload = %q", payload)
	}

	// Send a client ping; expect pong with same payload.
	if _, err := c.Write(makeClientFrame(opPing, []byte("p"))); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		op, payload = readWSFrame(t, br)
		if op == opPong {
			if string(payload) != "p" {
				t.Errorf("pong payload = %q", payload)
			}
			break
		}
		if op == opPing || op == opText {
			continue // skip noise
		}
		t.Fatalf("unexpected op=%d", op)
	}

	// Client close → server should reply with close.
	closeBody := make([]byte, 2)
	binary.BigEndian.PutUint16(closeBody, 1000)
	if _, err := c.Write(makeClientFrame(opClose, closeBody)); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		op, _ = readWSFrame(t, br)
		if op == opClose {
			break
		}
		if op == opPing || op == opPong || op == opText {
			continue
		}
		t.Fatalf("unexpected op=%d", op)
	}
}

func TestUpgradeWSRejectsNonUpgrade(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	if _, _, err := upgradeWS(w, r); err == nil {
		t.Error("expected error on plain GET")
	}
}

// fakeHijacker lets us check headers in upgradeWS without a real conn.
type fakeHijacker struct {
	*httptest.ResponseRecorder
	pipe net.Conn
	hjConn net.Conn
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return f.hjConn, bufio.NewReadWriter(bufio.NewReader(f.hjConn), bufio.NewWriter(f.hjConn)), nil
}

func TestUpgradeWSAcceptsValidHandshake(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	server, client := net.Pipe()
	defer client.Close()
	w := &fakeHijacker{ResponseRecorder: httptest.NewRecorder(), hjConn: server}

	go func() {
		conn, _, err := upgradeWS(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
		}
		if conn != nil {
			conn.Close()
		}
	}()

	br := bufio.NewReader(client)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if statusLine != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Errorf("status: %q", statusLine)
	}
}

// helper to silence unused-import warnings in trimmed test sets
var _ = bytes.NewReader
var _ = io.Discard
var _ = sha1.New
var _ = base64.StdEncoding
var _ = fmt.Sprintf
