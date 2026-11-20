package main

import (
	"encoding/json"
	"io"
)

// readyLinePrefix starts the single stdout line a parent process (the desktop
// app) waits for. Logs go to stderr, so stdout carries nothing else.
const readyLinePrefix = "DUNGERRA_READY "

type readyInfo struct {
	// Addr is the bound listener address, e.g. "127.0.0.1:53123" or
	// "0.0.0.0:8080". With port 0 it reveals the port that was picked.
	Addr string `json:"addr"`
}

// formatReadyLine returns the line announcing that the server accepts connections.
func formatReadyLine(addr string) string {
	data, _ := json.Marshal(readyInfo{Addr: addr}) // cannot fail for a string field
	return readyLinePrefix + string(data) + "\n"
}

// closedOnEOF returns a channel that is closed once r is exhausted or fails.
// The desktop app keeps the server's stdin open for as long as it runs, so
// EOF means the app quit or crashed, and the server should save and stop.
// Unlike a signal, this also works on Windows.
func closedOnEOF(r io.Reader) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, r)
	}()
	return done
}
