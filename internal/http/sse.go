package httpserver

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

type SSEBroker struct {
	clients    map[chan string]bool
	newClients chan chan string
	defunct    chan chan string
	messages   chan string
	mutex      sync.Mutex
}

func NewSSEBroker() *SSEBroker {
	b := &SSEBroker{
		clients:    make(map[chan string]bool),
		newClients: make(chan chan string),
		defunct:    make(chan chan string),
		messages:   make(chan string),
	}
	go b.start()
	return b
}

func (b *SSEBroker) start() {
	for {
		select {
		case s := <-b.newClients:
			b.mutex.Lock()
			b.clients[s] = true
			b.mutex.Unlock()
			log.Println("Added new SSE client")

		case s := <-b.defunct:
			b.mutex.Lock()
			delete(b.clients, s)
			close(s)
			b.mutex.Unlock()
			log.Println("Removed SSE client")

		case msg := <-b.messages:
			b.mutex.Lock()
			for s := range b.clients {
				select {
				case s <- msg:
				default:
					// Client is blocked, skip
				}
			}
			b.mutex.Unlock()
		}
	}
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan string)
	b.newClients <- messageChan

	notify := r.Context().Done()

	go func() {
		<-notify
		b.defunct <- messageChan
	}()

	for {
		msg, open := <-messageChan
		if !open {
			break
		}
		fmt.Fprintf(w, "data: %s\n\n", msg)
		w.(http.Flusher).Flush()
	}
}

func (b *SSEBroker) Broadcast(msg string) {
	b.messages <- msg
}
