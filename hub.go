package gowebsockethub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	conn     *websocket.Conn
	isServer bool

	err      chan error
	canceled chan struct{}
	reading  chan Message
	writing  chan Message
	handlers map[MessageType]Handler

	// https://github.com/gorilla/websocket/blob/666c197fc9157896b57515c3a3326c3f8c8319fe/examples/chat/client.go
	pingPeriod     time.Duration
	readWait       time.Duration // pongWait
	writeWait      time.Duration
	maxMessageSize int64
}

func New(conn *websocket.Conn, isServer bool, readingCapacity uint32, writingCapacity uint32) (hub *Hub, writing chan Message, err chan error) {
	hub = &Hub{
		conn:     conn,
		isServer: isServer,

		err:      make(chan error),
		canceled: make(chan struct{}, 3),
		reading:  make(chan Message, readingCapacity),
		writing:  make(chan Message, writingCapacity),
		handlers: make(map[MessageType]Handler),

		pingPeriod:     time.Second * 60 * 9 / 10,
		readWait:       time.Second * 60,
		writeWait:      time.Second * 10,
		maxMessageSize: 512,
	}

	return hub, hub.writing, hub.err
}

func (hub *Hub) SetPingPeriod(period time.Duration) {
	hub.pingPeriod = period
}

func (hub *Hub) SetReadWait(wait time.Duration) {
	hub.readWait = wait
}

func (hub *Hub) SetWriteWait(wait time.Duration) {
	hub.writeWait = wait
}

func (hub *Hub) SetMaxMessageSize(size int64) {
	hub.maxMessageSize = size
}

func (hub *Hub) Route(mt MessageType, handler Handler) {
	if _, ok := hub.handlers[mt]; ok {
		panic(fmt.Sprintf("handler already exists: %s", mt))
	}

	hub.handlers[mt] = handler
}

func (hub *Hub) Run(ctx context.Context, wg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(ctx)
	subwg := &sync.WaitGroup{}

	subwg.Add(1)
	go hub.read(ctx, subwg)

	subwg.Add(1)
	go hub.write(ctx, subwg)

	subwg.Add(1)
	go hub.handle(ctx, subwg)

	<-hub.canceled

	cancel()
	subwg.Wait()
	hub.conn.Close()

	close(hub.err)
	close(hub.canceled)
	close(hub.reading)
	close(hub.writing)

	if wg != nil {
		wg.Done()
	}
}

func (hub *Hub) read(ctx context.Context, wg *sync.WaitGroup) {
	tag := "gowebsockethub.Hub.read"

	var (
		err     error
		message Message
	)

	defer func() {
		if err != nil {
			//
		}

		hub.canceled <- struct{}{}
		wg.Done()
	}()

	hub.conn.SetReadLimit(hub.maxMessageSize)

	err = hub.conn.SetReadDeadline(time.Now().Add(hub.readWait))
	if err != nil {
		err = fmt.Errorf("[%s] set read deadline: %w", tag, err)
		hub.err <- err
		return
	}

	hub.conn.SetPongHandler(func(string) error {
		err := hub.conn.SetReadDeadline(time.Now().Add(hub.readWait))
		if err != nil {
			err = fmt.Errorf("[%s] set read deadline in pong handler: %w", tag, err)
			hub.err <- err
			return err
		}

		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, b, e := hub.conn.ReadMessage()
			if e != nil {
				err = fmt.Errorf("[%s] read message: %w", tag, e)
				hub.err <- err
				return
			}

			e = json.Unmarshal(b, &message)
			if e != nil {
				hub.err <- fmt.Errorf("[%s] unmarshal message: %w", tag, e)
				continue
			}

			hub.reading <- message
		}
	}
}

func (hub *Hub) write(ctx context.Context, wg *sync.WaitGroup) {
	tag := "gowebsockethub.Hub.write"

	var err error

	defer func() {
		if err != nil {
			err = hub.conn.WriteMessage(websocket.CloseMessage, []byte{})
			if err != nil {
				hub.err <- fmt.Errorf("[%s] write close message: %w", tag, err)
			}
		}

		hub.canceled <- struct{}{}
		wg.Done()
	}()

	ticker := time.NewTicker(hub.pingPeriod)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e := hub.conn.SetWriteDeadline(time.Now().Add(hub.writeWait))
			if e != nil {
				err = fmt.Errorf("[%s] set write deadline: %w", tag, e)
				hub.err <- err
				return
			}

			e = hub.conn.WriteMessage(websocket.PingMessage, nil)
			if e != nil {
				err = fmt.Errorf("[%s] write ping message: %w", tag, e)
				hub.err <- err
				return
			}
		case message, ok := <-hub.writing:
			if !ok {
				err = fmt.Errorf("[%s] read writing chan", tag)
				hub.err <- err
				return
			}

			e := hub.conn.SetWriteDeadline(time.Now().Add(hub.writeWait))
			if e != nil {
				err = fmt.Errorf("[%s] set write deadline: %w", tag, e)
				hub.err <- err
				return
			}

			b, e := json.Marshal(message)
			if e != nil {
				hub.err <- fmt.Errorf("[%s] marshal message: %w", tag, e)
				continue
			}

			e = hub.conn.WriteMessage(websocket.TextMessage, b)
			if e != nil {
				err = fmt.Errorf("[%s] write text message: %w", tag, e)
				hub.err <- err
				return
			}
		}
	}
}

func (hub *Hub) handle(ctx context.Context, wg *sync.WaitGroup) {
	tag := "gowebsockethub.Hub.handle"

	var err error

	defer func() {
		if err != nil {
			//
		}

		hub.canceled <- struct{}{}
		wg.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-hub.reading:
			if !ok {
				err = fmt.Errorf("[%s] read reading chan", tag)
				hub.err <- err
				return
			}

			handler, ok := hub.handlers[message.Type]
			if !ok {
				hub.err <- fmt.Errorf("[%s] %s handler not found", tag, message.Type)
				continue
			}

			e := handler(ctx, message, hub.writing)
			if e != nil {
				hub.err <- fmt.Errorf("[%s] run %s handler: %w", tag, message.Type, e)
				continue
			}
		}
	}
}
