// Package mcpstdio provides bounded line framing for MCP standard IO messages.
package mcpstdio

import (
	"bufio"
	"errors"
)

const MaxMessageBytes = 1 << 20

var ErrMessageTooLarge = errors.New("MCP message exceeds the size limit")

func ReadLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > MaxMessageBytes {
			return nil, ErrMessageTooLarge
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}
