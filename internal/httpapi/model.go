package httpapi

import "time"

type Conversation struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  []Message `json:"messages,omitempty"`
}

type Message struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversationId"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	ClientMessageID string    `json:"clientMessageId,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type MessageInput struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
}
