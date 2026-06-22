package sse

import (
	"bytes"
)

type LogWriter struct {
	stream *Stream
	prefix string
	buf    bytes.Buffer
}

func NewLogWriter(stream *Stream, prefix string) *LogWriter {
	return &LogWriter{
		stream: stream,
		prefix: prefix,
	}
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	w.buf.Write(p)

	for {
		line, err := w.buf.ReadBytes('\n')
		if err != nil {
			w.buf.Write(line)
			break
		}

		text := string(line)
		if len(text) > 0 && text[len(text)-1] == '\n' {
			text = text[:len(text)-1]
		}
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
		}
		if text == "" {
			continue
		}

		w.stream.PublishData(w.prefix + text)
	}

	return n, nil
}

func (w *LogWriter) Flush() {
	if w.buf.Len() > 0 {
		text := w.buf.String()
		w.buf.Reset()
		if text != "" {
			w.stream.PublishData(w.prefix + text)
		}
	}
}
