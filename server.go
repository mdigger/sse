package sse

import (
	"fmt"
	"mime"
	"net/http"
	"sync/atomic"
	"time"
)

const Mimetype = "text/event-stream"

// Broker обеспечивает поддержку Server Side Events.
type Broker struct {
	register   chan<- chan<- string // канал подключения новых клиентов
	unregister chan<- chan<- string // канал отключения клиентов
	notifier   chan<- eventSourcer  // канал приема событий на отправку
	counter    uint32               // счетчик подключенных
	// OnLastIDRequest func(id string) []Event     // вызывается при запросе событий с последнего идентификатора
}

// New инициализирует и возвращает новый брокер с поддержкой Server Side Events.
func New() *Broker {
	var (
		notifier   = make(chan eventSourcer, 1)       // канал отправки событий
		register   = make(chan chan<- string)         // канал приема каналов новых клиентов
		unregister = make(chan chan<- string)         // канал для приема закрытия канала
		clients    = make(map[chan<- string]struct{}) // список текущих клиентов
	)
	var broker = &Broker{
		notifier:   notifier,
		register:   register,
		unregister: unregister,
	}
	go func() {
		for {
			select {
			case client := <-register: // подключился новый клиент
				if _, ok := clients[client]; !ok {
					clients[client] = struct{}{}
					atomic.AddUint32(&broker.counter, 1)
					// log.Warning("~ connected")
				}
			case client := <-unregister: // отключился клиент
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client)
					atomic.AddUint32(&broker.counter, ^uint32(0))
					// log.Warning("~ disconnected")
				}
			case event := <-notifier: // отправить событие всем клиентам
				data := event.data() // преобразуем к формату EventSource
				for client := range clients {
					client <- data
				}
				// log.Warning("~ data")
			}
		}
	}()
	return broker
}

// Connected возвращает количество подключенных клиентов.
func (broker *Broker) Connected() int {
	return int(atomic.LoadUint32(&broker.counter))
}

// Data формирует и отправляет данные всем клиентам. В качестве параметров
// указывается название события, данные и идентификатор. Любое из этих полей
// может быть пустым.
func (broker *Broker) Data(name, data, id string) {
	broker.notifier <- Event{
		Name: name,
		Data: data,
		ID:   id,
	}
}

// Send отправляет всем зарегистрированым клиентам указанное событие.
func (broker *Broker) Send(event Event) {
	broker.notifier <- event
}

// Comment отправляет всем зарегистрированным клиентам комментарий.
func (broker *Broker) Comment(message string) {
	broker.notifier <- Comment(message)
}

// Retry отправляет всем клиентам указание задержки восстановления соединения.
func (broker *Broker) Retry(through time.Duration) {
	if through > 0 {
		broker.notifier <- Retry(through)
	}
}

func (broker *Broker) Close() {

}

// ServeHTTP обрабатывает серверное подключение клиента через HTTP.
func (broker *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// проверяем, что поддерживается частичная отдача данных
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported!", http.StatusInternalServerError)
		return
	}

	header := w.Header()
	mediatype, _, _ := mime.ParseMediaType(r.Header.Get("Accept"))
	if mediatype != Mimetype {
		header.Set("Accept", Mimetype)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	// устанавливаем в ответе заголовки
	header.Set("Content-Type", Mimetype)
	// header.Set("Cache-Control", "no-cache")
	// header.Set("Connection", "keep-alive")
	header.Set("Access-Control-Allow-Origin", "*")

	// отсылаем последние сообщения, начиная с указанного номера, если
	// это поддерживается брокером и запрашивается клиентом
	// if lastID := r.Header.Get("Last-Event-ID"); lastID != "" &&
	// 	broker.OnLastIDRequest != nil {
	// 	for _, event := range broker.OnLastIDRequest(lastID) {
	// 		if _, err := fmt.Fprintln(w, event.data()); err != nil {
	// 			return // в случае ошибки закрываем соединение
	// 		}
	// 		flusher.Flush() // принудительно сбрасываем буфер на отправление
	// 	}
	// }

	// инициализируем канал для приема событий
	messages := make(chan string)
	// отправляем его серверу для регистрации нового клиента
	broker.register <- messages
	// при обрыве соединения тоже отправляем уведомление о закрытии
	go func() {
		<-r.Context().Done()
		// log.Warning("context done")
		broker.unregister <- messages
	}()
	// отправляем все входящие события и отправляем их клиенту
	for data := range messages {
		if _, err := fmt.Fprintln(w, data); err != nil {
			break
		}
		flusher.Flush() // принудительно сбрасываем буфер на отправление
	}
}
