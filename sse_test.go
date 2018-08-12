package sse

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSE(t *testing.T) {
	broker := new(Server)
	go func() {
		for range time.Tick(5 * time.Second) {
			broker.Event("", "timer", time.Now().Format("15:04:05"))
		}
	}()
	go func() {
		for range time.Tick(25 * time.Second) {
			broker.Comment("comment " + time.Now().Format("15:04:05"))
		}
	}()
	go func() {
		var id int
		for range time.Tick(7 * time.Second) {
			id++
			broker.Event(fmt.Sprintf("%04d", id), "event", &struct {
				ID   int       `json:"id"`
				Time time.Time `json:"time"`
			}{
				ID:   id,
				Time: time.Now().Truncate(time.Second),
			})
		}
	}()

	ts := httptest.NewServer(broker)
	defer ts.Close()

	fmt.Println("url:", ts.URL)

	req, _ := http.NewRequest("GET", ts.URL, nil)
	req.Header.Set("Accept", "text/event-stream")

	client := ts.Client()
	// client.Transport = &logTransport{http.DefaultTransport}

	res, err := client.Do(req)
	if err != nil {
		t.Error("http client error", err)
	}
	fmt.Println(res.Status)
	r := bufio.NewReader(res.Body)
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			t.Error("event source error", err)
			break
		}
		fmt.Print(s)
	}
	res.Body.Close()
	// time.Sleep(time.Second * 10)
}
