package main

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFormatReadyLine(t *testing.T) {
	// Port 0 is how the desktop app asks for a free port; the line must
	// carry the port that was actually bound.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	line := formatReadyLine(ln.Addr().String())
	if !strings.HasPrefix(line, readyLinePrefix) || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("malformed ready line %q", line)
	}
	var info readyInfo
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), readyLinePrefix)), &info); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	_, port, err := net.SplitHostPort(info.Addr)
	if err != nil || port == "0" || port == "" {
		t.Fatalf("addr %q does not carry the bound port", info.Addr)
	}
}

func TestClosedOnEOF(t *testing.T) {
	r, w := io.Pipe()
	done := closedOnEOF(r)

	w.Write([]byte("ignored input"))
	select {
	case <-done:
		t.Fatal("closed while stdin is still open")
	case <-time.After(50 * time.Millisecond):
	}

	w.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not closed after EOF")
	}
}
