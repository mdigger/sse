// Package sse provides HTML5 Server-Sent Events for Go.
//
// See http://www.w3.org/TR/eventsource/ for the technical specification.
package sse

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server provides HTML5 Server-Sent Events
type Server struct {
	clients map[chan string]struct{} // connected clients
	mu      sync.RWMutex
}

// Connected return number of connected clients.
func (s *Server) Connected() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

var (
	pool            = sync.Pool{New: func() interface{} { return new(strings.Builder) }}
	newlineReplacer = strings.NewReplacer("\n", "\\n")
)

// Event sends an event with the given data encoded as JSON to all connected
// clients.
func (s *Server) Event(id, name string, v interface{}) error {
	// converting the data to the JSON format, if necessary
	var data string
	switch v := v.(type) {
	case nil:
	case string:
		data = v
	case []byte:
		data = string(v)
	case json.RawMessage:
		data = string(v)
	case error:
		data = v.Error()
	default:
		d, err := json.Marshal(v)
		if err != nil {
			return err
		}
		data = string(d)
	}

	buf := pool.Get().(*strings.Builder)
	buf.Reset()

	if name != "" {
		fmt.Fprintln(buf, "event:", newlineReplacer.Replace(name))
	}
	if data != "" {
		for _, line := range strings.Split(data, "\n") {
			fmt.Fprintln(buf, "data:", line)
		}
	}
	if id != "" {
		fmt.Fprintln(buf, "id:", newlineReplacer.Replace(id))
	}

	s.send(buf.String())

	pool.Put(buf)

	return nil
}

// Comment sends an comment with the given text to all connected clients.
func (s *Server) Comment(text string) {
	buf := pool.Get().(*strings.Builder)
	buf.Reset()

	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintln(buf, ":", line)
	}

	s.send(buf.String())

	pool.Put(buf)
}

// Retry sends all clients an indication of the delay in restoring the connection.
func (s *Server) Retry(d time.Duration) {
	s.send(fmt.Sprintln("retry:", int64(d)/1000/1000))
}

// send sends data to all registered customers.
func (s *Server) send(data string) {
	s.mu.RLock()
	for client := range s.clients {
		client <- data
	}
	s.mu.RUnlock()
}

// Close closes the server and disconnect all clients.
func (s *Server) Close() {
	s.mu.Lock()
	for client := range s.clients {
		close(client)
	}
	s.mu.Unlock()
}

// mimetype specifies the data type for server events.
const mimetype = "text/event-stream"

// ServeHTTP implements http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	mediatype, _, _ := mime.ParseMediaType(r.Header.Get("Accept"))
	if mediatype != mimetype {
		w.Header().Set("Accept", mimetype)
		http.Error(w, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}

	w.Header().Set("Content-Type", mimetype)
	w.Header().Set("Cache-Control", "no-cache")

	messages := make(chan string) // channel for receiving events
	s.mu.Lock()
	if s.clients == nil {
		s.clients = make(map[chan string]struct{})
	}
	s.clients[messages] = struct{}{}
	s.mu.Unlock()

	done := r.Context().Done() // channel closure compound
	var closed bool            // flag that channel is already closed
loop:
	for {
		select {
		case data, ok := <-messages:
			if !ok {
				closed = true // bring the flag that the channel is already closed
				break loop
			}

			if _, err := fmt.Fprintln(w, data); err != nil {
				break loop
			}

			flusher.Flush() // forced reset buffer for departure

		case <-done:
			break loop
		}
	}

	s.mu.Lock()
	delete(s.clients, messages)
	s.mu.Unlock()

	if !closed {
		close(messages)
	}
}
