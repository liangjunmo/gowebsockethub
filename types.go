package gowebsockethub

import (
	"context"
)

type Route string

type Message struct {
	ID      string `json:"id"`
	Route   Route  `json:"route"`
	Payload string `json:"payload"`
}

type Handler func(ctx context.Context, message Message, writing chan<- Message) error
