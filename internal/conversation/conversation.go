package conversation

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("conversation not found")
	ErrArchived = errors.New("conversation archived")
)

type Conversation struct {
	ID         string
	Title      string
	Status     string
	Repository *RepositoryConfig
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Messages   []Message
}

type RepositoryConfig struct {
	URL      string
	Revision string
}

type Message struct {
	ID              string
	ConversationID  string
	Role            string
	Content         string
	ClientMessageID string
	Status          string
	CreatedAt       time.Time
}

type NewMessage struct {
	Content         string
	ClientMessageID string
}

type Repository interface {
	CreateConversation(ctx context.Context) (Conversation, error)
	GetConversation(ctx context.Context, id string) (Conversation, error)
	ListMessages(ctx context.Context, id string) ([]Message, error)
	CreateMessage(ctx context.Context, id string, input NewMessage) (Message, bool, error)
}

type Lister interface {
	ListConversations(context.Context) ([]Conversation, error)
}

type RepositoryConfigurer interface {
	CreateConversationWithRepository(context.Context, *RepositoryConfig) (Conversation, error)
}

type ActivityRecorder interface {
	TouchConversation(context.Context, string, time.Time) error
}

type RetentionStore interface {
	ArchiveInactiveConversations(context.Context, time.Time) error
}
