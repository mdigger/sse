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
	"sync/atomic"
	"time"
)

// Server provides HTML5 Server-Sent Events
type Server struct {
	clients   sync.Map // подключенные клиенты
	connected uint32   // счетчик подключений
}

// Connected return number of connected clients.
func (s *Server) Connected() int {
	return int(atomic.LoadUint32(&s.connected))
}

var pool = sync.Pool{New: func() interface{} { return new(strings.Builder) }}

// Event sends an event with the given data encoded as JSON to all connected
// clients.
func (s *Server) Event(id, name string, v interface{}) error {
	if atomic.LoadUint32(&s.connected) == 0 {
		return nil // если нет клиентов, то и не готовим данные
	}
	// преобразуем данные к формату JSON, если это необходимо
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
	// формируем форматированное описание события
	var buf = pool.Get().(*strings.Builder)
	buf.Reset()
	if name != "" {
		fmt.Fprintln(buf, "event:", strings.Replace(name, "\n", "\\n", -1))
	}
	if data != "" {
		for _, line := range strings.Split(data, "\n") {
			fmt.Fprintln(buf, "data:", line)
		}
	}
	if id != "" {
		fmt.Fprintln(buf, "id:", strings.Replace(id, "\n", "\\n", -1))
	}
	s.send(buf.String())
	pool.Put(buf)
	return nil
}

// Comment sends an comment with the given text to all connected clients.
func (s *Server) Comment(text string) {
	if atomic.LoadUint32(&s.connected) == 0 {
		return // если нет клиентов, то и не готовим данные
	}
	var buf = pool.Get().(*strings.Builder)
	buf.Reset()
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintln(buf, ":", line)
	}
	s.send(buf.String())
	pool.Put(buf)
}

// Retry sends all clients an indication of the delay in restoring the connection.
func (s *Server) Retry(d time.Duration) {
	if atomic.LoadUint32(&s.connected) == 0 {
		return // если нет клиентов, то и не готовим данные
	}
	s.send(fmt.Sprintln("retry:", int64(d/1000/1000)))
}

// send отправляет данные всем зарегистрированным клиентам.
func (s *Server) send(data string) {
	s.clients.Range(func(client, _ interface{}) bool {
		client.(chan string) <- data
		return true
	})
}

// Close closes the server and disconnect all clients.
func (s *Server) Close() {
	s.clients.Range(func(client, _ interface{}) bool {
		close(client.(chan string))
		return true
	})
}

// mimetype задает тип данных для серверных событий.
const mimetype = "text/event-stream"

// ServeHTTP implements http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// проверяем, что поддерживается частичная отдача данных
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	var header = w.Header()
	mediatype, _, _ := mime.ParseMediaType(r.Header.Get("Accept"))
	if mediatype != mimetype {
		header.Set("Accept", mimetype)
		var text = http.StatusText(http.StatusNotAcceptable)
		http.Error(w, text, http.StatusNotAcceptable)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	header.Set("Content-Type", mimetype)
	header.Set("Cache-Control", "no-cache")
	// header.Set("Access-Control-Allow-Origin", "*")
	var (
		messages = make(chan string)  // канал для приема событий
		done     = r.Context().Done() // канал закрытия соединения
		closed   bool                 // флаг, что канал уже закрыт
	)
	s.clients.Store(messages, nil)
	atomic.AddUint32(&s.connected, 1)
loop:
	for {
		select {
		case data, ok := <-messages:
			if !ok {
				closed = true // взводим флаг, что канал уже закрыт
				break loop
			}
			if _, err := fmt.Fprintln(w, data); err != nil {
				break loop
			}
			flusher.Flush() // принудительно сбрасываем буфер на отправление
		case <-done:
			break loop
		}
	}
	s.clients.Delete(messages)
	atomic.AddUint32(&s.connected, ^uint32(0))
	if !closed {
		close(messages)
	}
}
