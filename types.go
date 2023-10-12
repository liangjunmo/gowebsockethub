package gowebsockethub

import (
	"context"
)

type Route string

type Message interface {
	GetID() string
	GetRoute() Route
	GetPayload() string
}

type Parser func(raw []byte) (Message, error)

type Handler func(ctx context.Context, message Message, writing chan<- Message) error
