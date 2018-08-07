package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBroker(t *testing.T) {
	broker := New()
	go func() {
		for range time.Tick(5 * time.Second) {
			broker.Data("timer", time.Now().Format("15:04:05"), "")
		}
	}()
	go func() {
		for range time.Tick(25 * time.Second) {
			broker.Comment("comment " + time.Now().Format("15:04:05"))
		}
	}()
	go func() {
		var id uint64
		for range time.Tick(7 * time.Second) {
			id++
			var str = &struct {
				ID   uint64    `json:"id"`
				Time time.Time `json:"time,omitempty"`
			}{ID: id, Time: time.Now()}
			data, err := json.MarshalIndent(str, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			broker.Data("payload\ndata", string(data), fmt.Sprintf("id%03d", id))
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
