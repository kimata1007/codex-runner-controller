package httpapi

type Server struct {
	store ConversationRepository
}

func NewServer() *Server {
	return NewServerWithRepository(&Store{conversations: make(map[string]*Conversation)})
}

func NewServerWithRepository(repository ConversationRepository) *Server {
	return &Server{store: repository}
}
