package gowebsockethub_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"

	"github.com/liangjunmo/gowebsockethub"
)

const (
	addr  = "localhost:8080"
	route = "/ws"

	MessageTypeTestRequest  gowebsockethub.MessageType = "MessageTypeTestRequest"
	MessageTypeTestResponse gowebsockethub.MessageType = "MessageTypeTestResponse"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func TestHub(t *testing.T) {
	http.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		assert.Nil(t, err)

		hub, _, errs := gowebsockethub.New(conn, true, 10, 10)

		go func() {
			for err := range errs {
				t.Log(err)
				assert.NotNil(t, err)
				return
			}
		}()

		hub.Route(MessageTypeTestRequest, func(ctx context.Context, message gowebsockethub.Message, writing chan<- gowebsockethub.Message) error {
			t.Logf("%+v", message)

			writing <- gowebsockethub.Message{
				ID:      time.Now().String(),
				Type:    MessageTypeTestResponse,
				Payload: message.Payload + "...",
			}

			return nil
		})

		hub.Run(r.Context(), nil)
	})

	server := &http.Server{
		Addr: addr,
	}

	go func() {
		err := server.ListenAndServe()
		if err != http.ErrServerClosed {
			t.Error(err)
			return
		}
	}()

	u := url.URL{Host: addr, Scheme: "ws", Path: route}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	assert.Nil(t, err)
	defer conn.Close()

	hub, _, errs := gowebsockethub.New(conn, false, 10, 10)

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	go func() {
		go func() {
			for err := range errs {
				t.Log(err)
				assert.NotNil(t, err)
				return
			}
		}()
	}()

	hub.Route(MessageTypeTestResponse, func(ctx context.Context, message gowebsockethub.Message, writing chan<- gowebsockethub.Message) error {
		t.Logf("%+v", message)

		done <- struct{}{}

		return nil
	})

	wg.Add(1)
	go func() {
		hub.Run(ctx, wg)
	}()

	message := gowebsockethub.Message{
		ID:      time.Now().String(),
		Type:    MessageTypeTestRequest,
		Payload: "hello world",
	}

	b, _ := json.Marshal(message)

	err = conn.WriteMessage(websocket.TextMessage, b)
	assert.Nil(t, err)

	<-done

	cancel()
	conn.Close()
	wg.Wait()

	err = server.Shutdown(ctx)
	assert.Nil(t, err)
}