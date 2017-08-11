package sse

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mdigger/log"
)

const Mimetype = "text/event-stream"

// Broker обеспечивает поддержку Server Side Events.
type Broker struct {
	register   chan<- chan<- string       // канал подключения новых клиентов
	unregister chan<- chan<- string       // канал отключения клиентов
	notifier   chan<- eventSourcer        // канал приема событий на отправку
	clients    map[chan<- string]struct{} //  список подключенных клиентов
}

// New инициализирует и возвращает новый брокер с поддержкой Server Side Events.
func New() *Broker {
	var (
		notifier   = make(chan eventSourcer, 1)       // канал отправки событий
		register   = make(chan chan<- string)         // канал приема каналов новых клиентов
		unregister = make(chan chan<- string)         // канал для приема закрытия канала
		clients    = make(map[chan<- string]struct{}) // список текущих клиентов
	)
	go func() {
		for {
			select {
			case client := <-register: // подключился новый клиент
				clients[client] = struct{}{}
				log.WithField("connected", len(clients)).Info("new client connection")
			case client := <-unregister: // отключился клиент
				delete(clients, client)
				log.WithField("connected", len(clients)).Info("client disconnected")
			case event := <-notifier: // отправить событие всем клиентам
				data := event.data() // преобразуем к формату EventSource
				for client := range clients {
					client <- data
				}
				if log.GetLevel() < 0 {
					ctxlog := log.WithField("length", len(data))
					switch event := event.(type) {
					case Event:
						ctxlog = ctxlog.WithFields(log.Fields{
							"type": "event",
							"name": event.Name,
							"id":   event.ID})
					case Comment:
						ctxlog = ctxlog.WithField("type", "comment")
					case Retry:
						ctxlog = ctxlog.WithFields(log.Fields{
							"type":     "retry",
							"duration": time.Duration(event)})
					}
					ctxlog.Debug("sending event")
				}
			}
		}
	}()
	return &Broker{
		notifier:   notifier,
		register:   register,
		unregister: unregister,
		clients:    clients,
	}
}

// Connected возвращает количество подключенных клиентов.
func (broker *Broker) Connected() int {
	return len(broker.clients)
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
	if r.Header.Get("Accept") != Mimetype {
		header.Set("Accept", Mimetype)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	// устанавливаем в ответе заголовки
	header.Set("Content-Type", Mimetype)
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("Access-Control-Allow-Origin", "*")
	// инициализируем канал для приема событий
	messages := make(chan string)
	// отправляем его серверу для регистрации нового клиента
	broker.register <- messages
	// при обрыве соединения тоже отправляем уведомление о закрытии
	go func() {
		<-r.Context().Done()
		broker.unregister <- messages
	}()
	// отправляем все входящие события и отправляем их клиенту
	for data := range messages {
		if _, err := fmt.Fprintln(w, data); err != nil {
			break
		}
		flusher.Flush() // принудительно сбрасываем буфер на отправление
	}
	// по закрытии соединения отправляем этот канал для закрытия
	broker.unregister <- messages
}
