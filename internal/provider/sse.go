package provider

import (
	"bufio"
	"bytes"
	"io"
)

// sseEvent is a single decoded Server-Sent Event.
type sseEvent struct {
	Event string
	Data  []byte
}

// sseScanner reads an SSE byte stream and yields events. It handles multi-line
// data fields and blank-line event terminators per the SSE spec, which is the
// framing both OpenAI and Anthropic use for streaming responses.
type sseScanner struct {
	r   *bufio.Reader
	buf bytes.Buffer
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{r: bufio.NewReaderSize(r, 64*1024)}
}

// next returns the next event, or io.EOF at end of stream.
func (s *sseScanner) next() (sseEvent, error) {
	var ev sseEvent
	s.buf.Reset()
	sawData := false
	for {
		line, err := s.r.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			switch {
			case len(trimmed) == 0:
				// Blank line: dispatch the accumulated event if any.
				if sawData || ev.Event != "" {
					ev.Data = append([]byte(nil), s.buf.Bytes()...)
					return ev, nil
				}
			case bytes.HasPrefix(trimmed, []byte(":")):
				// Comment line, ignore.
			case bytes.HasPrefix(trimmed, []byte("data:")):
				sawData = true
				chunk := bytes.TrimPrefix(trimmed, []byte("data:"))
				chunk = bytes.TrimPrefix(chunk, []byte(" "))
				if s.buf.Len() > 0 {
					s.buf.WriteByte('\n')
				}
				s.buf.Write(chunk)
			case bytes.HasPrefix(trimmed, []byte("event:")):
				name := bytes.TrimPrefix(trimmed, []byte("event:"))
				ev.Event = string(bytes.TrimSpace(name))
			}
		}
		if err != nil {
			if err == io.EOF && (sawData || ev.Event != "") {
				ev.Data = append([]byte(nil), s.buf.Bytes()...)
				return ev, nil
			}
			return sseEvent{}, err
		}
	}
}
