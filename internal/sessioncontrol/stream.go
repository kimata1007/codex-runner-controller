package sessioncontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

const (
	Path          = "/v1/session/control"
	MediaType     = "application/vnd.codex.session-control.v1+ndjson"
	MaxFrameBytes = 1 << 20
	writeTimeout  = 10 * time.Second
)

type Kind string

const (
	Command   Kind = "command"
	Reply     Kind = "reply"
	Event     Kind = "event"
	Ack       Kind = "ack"
	Heartbeat Kind = "heartbeat"
)

type Frame struct {
	Kind     Kind            `json:"kind"`
	ID       string          `json:"id,omitempty"`
	Method   string          `json:"method,omitempty"`
	Sequence uint64          `json:"sequence,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func (f Frame) valid() bool {
	if len(f.ID) > 128 || len(f.Method) > 128 || len(f.Error) > 1024 {
		return false
	}
	switch f.Kind {
	case Command:
		return f.ID != "" && f.Method != "" && f.Sequence == 0 && f.Error == ""
	case Reply:
		return f.ID != "" && f.Method == "" && f.Sequence == 0 && (f.Error == "" || len(f.Payload) == 0)
	case Event:
		return f.ID != "" && f.Method != "" && f.Sequence > 0 && f.Error == "" && len(f.Payload) != 0
	case Ack:
		return f.ID != "" && f.Method == "" && f.Sequence > 0 && f.Error == "" && len(f.Payload) == 0
	case Heartbeat:
		return f.ID == "" && f.Method == "" && f.Sequence == 0 && f.Error == "" && len(f.Payload) == 0
	default:
		return false
	}
}

// Stream carries concurrent sends and a single receive loop. The caller
// validates frame direction and binds command IDs to the authorized session.
type Stream struct {
	reader *bufio.Scanner
	write  func([]byte) error
	mu     sync.Mutex
}

func newStream(reader io.Reader, write func([]byte) error) *Stream {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes+1)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) != 0 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		return 0, nil, nil
	})
	return &Stream{reader: scanner, write: write}
}

func (s *Stream) Receive() (Frame, error) {
	if !s.reader.Scan() {
		if err := s.reader.Err(); err != nil {
			return Frame{}, err
		}
		return Frame{}, io.EOF
	}
	data := s.reader.Bytes()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var frame Frame
	if !json.Valid(data) || decoder.Decode(&frame) != nil || !frame.valid() {
		return Frame{}, errors.New("invalid session control frame")
	}
	return frame, nil
}

func (s *Stream) Send(frame Frame) error {
	if !frame.valid() {
		return errors.New("invalid session control frame")
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if len(data) > MaxFrameBytes {
		return errors.New("session control frame exceeds the size limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(append(data, '\n'))
}

type Connection struct {
	*Stream
	response io.ReadCloser
	request  *io.PipeWriter
	cancel   context.CancelFunc
}

func (c *Connection) Close() {
	c.cancel()
	_ = c.request.Close()
	_ = c.response.Close()
}

// Dial opens a stream using the client's authorized Session Pod transport.
// The context governs the lifetime of the established connection.
func Dial(ctx context.Context, transport *http.Transport) (*Connection, error) {
	ctx, cancel := context.WithCancel(ctx)
	input, output := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://session-control"+Path, input)
	if err != nil {
		cancel()
		_ = output.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", MediaType)
	req.Header.Set("Accept", MediaType)
	// RoundTrip keeps the request on this transport without redirects or cookies.
	response, err := transport.RoundTrip(req)
	if err != nil {
		cancel()
		_ = output.Close()
		return nil, err
	}
	if response.StatusCode != http.StatusOK || !hasMediaType(response.Header.Get("Content-Type")) {
		cancel()
		_ = output.Close()
		_ = response.Body.Close()
		return nil, fmt.Errorf("session control handshake rejected (%d)", response.StatusCode)
	}
	stream := newStream(response.Body, func(data []byte) error {
		_, err := output.Write(data)
		return err
	})
	return &Connection{Stream: stream, response: response.Body, request: output, cancel: cancel}, nil
}

// Serve enables HTTP/1.1 full duplex before either side sends a frame. The
// caller owns admission to the endpoint and the lifetime of its frame loops.
func Serve(w http.ResponseWriter, r *http.Request, handle func(context.Context, *Stream)) {
	if r.Method != http.MethodPost || r.URL.Path != Path || r.URL.RawQuery != "" ||
		!hasMediaType(r.Header.Get("Content-Type")) || !hasMediaType(r.Header.Get("Accept")) {
		http.Error(w, "invalid session control request", http.StatusBadRequest)
		return
	}
	control := http.NewResponseController(w)
	if err := control.EnableFullDuplex(); err != nil {
		http.Error(w, "session control requires full duplex", http.StatusInternalServerError)
		return
	}
	if err := control.SetReadDeadline(time.Time{}); err != nil {
		http.Error(w, "session control could not configure the connection", http.StatusInternalServerError)
		return
	}
	if err := control.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		http.Error(w, "session control could not configure the connection", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", MediaType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := control.Flush(); err != nil {
		return
	}
	if err := control.SetWriteDeadline(time.Time{}); err != nil {
		return
	}
	stream := newStream(r.Body, func(data []byte) error {
		if err := control.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := control.Flush(); err != nil {
			return err
		}
		return control.SetWriteDeadline(time.Time{})
	})
	handle(r.Context(), stream)
}

func hasMediaType(header string) bool {
	value, _, err := mime.ParseMediaType(header)
	return err == nil && value == MediaType
}
