package sse_test

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mdigger/sse"
)

func Example() {
	type Event struct {
		ID   int       `json:"id"`
		Time time.Time `json:"time"`
	}

	var sse = new(sse.Server)
	go func() {
		var id int
		for range time.Tick(5 * time.Second) {
			id++
			sse.Event(fmt.Sprintf("%04d", id), "event", &Event{
				ID:   id,
				Time: time.Now().Truncate(time.Second),
			})
		}
	}()
	http.Handle("/events", sse)
	log.Fatal(http.ListenAndServe(":8000", nil))
}
