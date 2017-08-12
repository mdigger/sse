package sse

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"
)

// eventSourcer описывает интерфейс, который должны поддерживать все элементы
// EventSource.
type eventSourcer interface {
	data() string
}

// Event описывает формат события для EventSource.
type Event struct {
	Name string // название события
	Data string // данные
	ID   string // уникальный идентификатор
}

// String возвращает описание события в формате EventSource.
func (e Event) data() string {
	var buf = bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if e.Name != "" {
		fmt.Fprintln(buf, "event:", strings.Replace(e.Name, "\n", "\\n", -1))
	}
	if e.Data != "" {
		for _, line := range strings.Split(e.Data, "\n") {
			fmt.Fprintln(buf, "data:", line)
		}
	}
	if e.ID != "" {
		fmt.Fprintln(buf, "id:", strings.Replace(e.ID, "\n", "\\n", -1))
	}
	return buf.String()
}

// Comment описывает комментарий для EventSource.
type Comment string

// Event возвращает строковое представление для комментария в формате EventSource.
func (c Comment) data() string {
	var buf = bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	for _, line := range strings.Split(string(c), "\n") {
		fmt.Fprintln(buf, ":", line)
	}
	return buf.String()
}

// Retry описывает время восстановления задержки переподключения.
type Retry time.Duration

// Event возвращает время восстановления задержки переподключения в формате
// EventSource.
func (r Retry) data() string {
	return fmt.Sprintln("retry:", int64(r))
}

var bufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}
