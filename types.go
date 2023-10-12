package gowebsockethub

import (
	"context"
)

type MessageType string

type Message struct {
	ID      string      `json:"id"`
	Type    MessageType `json:"type"`
	Payload string      `json:"payload"`
}

type Handler func(ctx context.Context, message Message, writing chan<- Message) error
