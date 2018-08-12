package sse

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server обеспечивает поддержку Server Side Events.
type Server struct {
	clients   sync.Map // подключенные клиенты
	connected uint32   // счетчик подключений
}

// Connected возвращает количество подключенных клиентов.
func (s *Server) Connected() uint32 {
	return s.connected
}

var pool = sync.Pool{New: func() interface{} { return new(strings.Builder) }}

// Event отправляет данные о событии.
func (s *Server) Event(name string, v interface{}, id string) error {
	if s.connected == 0 {
		return nil // если нет клиентов, то и не готовим данные
	}
	// преобразуем данные к формату JSON, если это необходимо
	var data string
	switch v := v.(type) {
	case string:
		data = v
	case []byte:
		data = string(v)
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

// Comment отправляет комментарий.
func (s *Server) Comment(text string) {
	if s.connected == 0 {
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

// Retry отсылает время восстановления задержки переподключения.
func (s *Server) Retry(d time.Duration) {
	if s.connected == 0 {
		return // если нет клиентов, то и не готовим данные
	}
	s.send(fmt.Sprintln("retry:", int64(d)))
}

// send отправляет данные всем зарегистрированным клиентам.
func (s *Server) send(data string) {
	s.clients.Range(func(client, _ interface{}) bool {
		client.(chan string) <- data
		return true
	})
}

// Close закрывает все соединения.
func (s *Server) Close() {
	s.clients.Range(func(client, _ interface{}) bool {
		close(client.(chan string))
		return true
	})
}

// mimetype задает тип данных для серверных событий.
const mimetype = "text/event-stream"

// ServeHTTP обрабатывает серверное подключение клиента через HTTP.
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
	var (
		messages = make(chan string)  // канал для приема событий
		done     = r.Context().Done() // канал закрытия соединения
		closed   bool                 // флаг, что канал уже закрыт
	)
	s.clients.Store(messages, nil)
	atomic.AddUint32(&s.connected, 1)
	log.Println("connected:", s.connected)
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
	log.Println("disconnected:", s.connected)
	if !closed {
		close(messages)
	}
}
