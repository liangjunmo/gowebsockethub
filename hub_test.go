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

type Message struct {
	ID          string               `json:"id"`
	MessageType gowebsockethub.Route `json:"type"`
	Payload     string               `json:"payload"`
}

func (m Message) GetID() string {
	return m.ID
}

func (m Message) GetRoute() gowebsockethub.Route {
	return m.MessageType
}

func (m Message) GetPayload() string {
	return m.Payload
}

var Parser gowebsockethub.Parser = func(raw []byte) (gowebsockethub.Message, error) {
	var message Message

	err := json.Unmarshal(raw, &message)
	if err != nil {
		return Message{}, err
	}

	return message, nil
}

const (
	addr  = "localhost:8080"
	route = "/ws"

	RouteTestRequest  gowebsockethub.Route = "TestRequest"
	RouteTestResponse gowebsockethub.Route = "TestResponse"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func TestHub(t *testing.T) {
	http.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		assert.Nil(t, err)

		hub, _, errs := gowebsockethub.New(conn, true, 10, 10, Parser)

		go func() {
			for err := range errs {
				t.Log(err)
				assert.NotNil(t, err)
				return
			}
		}()

		hub.Intercept(RouteTestRequest, func(ctx context.Context, message gowebsockethub.Message, writing chan<- gowebsockethub.Message) error {
			t.Logf("%+v", message)

			writing <- Message{
				ID:          time.Now().String(),
				MessageType: RouteTestResponse,
				Payload:     message.GetPayload() + "...",
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

	hub, _, errs := gowebsockethub.New(conn, false, 10, 10, Parser)

	go func() {
		for err := range errs {
			t.Log(err)
			assert.NotNil(t, err)
			return
		}
	}()

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	hub.Handle(RouteTestResponse, func(ctx context.Context, message gowebsockethub.Message, writing chan<- gowebsockethub.Message) error {
		t.Logf("%+v", message)

		done <- struct{}{}

		return nil
	})

	wg.Add(1)
	go func() {
		hub.Run(ctx, wg)
	}()

	message := Message{
		ID:          time.Now().String(),
		MessageType: RouteTestRequest,
		Payload:     "hello world",
	}

	b, _ := json.Marshal(message)

	err = conn.WriteMessage(websocket.TextMessage, b)
	assert.Nil(t, err)

	<-done

	conn.Close()
	cancel()
	wg.Wait()

	err = server.Shutdown(ctx)
	assert.Nil(t, err)
}
