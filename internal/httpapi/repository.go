package httpapi

type ConversationRepository interface {
	CreateConversation() *Conversation
	GetConversation(id string) (*Conversation, bool)
	ListMessages(id string) ([]Message, bool)
	CreateMessage(id string, input MessageInput) (Message, bool, error)
}
