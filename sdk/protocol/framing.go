package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const maxHeaderBytes = 8 << 10

var (
	ErrFrameTooLarge = errors.New("mensagem excede o limite do protocolo")
	ErrInvalidFrame  = errors.New("frame de protocolo inválido")
)

// Encoder escreve mensagens com framing Content-Length. É seguro para uso
// concorrente porque snapshots e respostas de host podem terminar juntos.
type Encoder struct {
	w   io.Writer
	mu  sync.Mutex
	max int
}

func NewEncoder(w io.Writer) *Encoder { return NewEncoderSize(w, MaxMessageSize) }

func NewEncoderSize(w io.Writer, maxSize int) *Encoder {
	if maxSize <= 0 {
		maxSize = MaxMessageSize
	}
	return &Encoder{w: w, max: maxSize}
}

func (e *Encoder) Write(message Message) error {
	if message.Version <= 0 || message.Sequence == 0 || message.Method == "" {
		return fmt.Errorf("%w: envelope incompleto", ErrInvalidFrame)
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("codificar mensagem: %w", err)
	}
	if len(body) > e.max {
		return fmt.Errorf("%w: %d > %d bytes", ErrFrameTooLarge, len(body), e.max)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	header := []byte("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n")
	if err := writeAll(e.w, header); err != nil {
		return err
	}
	return writeAll(e.w, body)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// Decoder lê uma sequência de frames, inclusive quando headers e corpos
// chegam fragmentados em vários reads.
type Decoder struct {
	r   *bufio.Reader
	max int
}

func NewDecoder(r io.Reader) *Decoder { return NewDecoderSize(r, MaxMessageSize) }

func NewDecoderSize(r io.Reader, maxSize int) *Decoder {
	if maxSize <= 0 {
		maxSize = MaxMessageSize
	}
	return &Decoder{r: bufio.NewReaderSize(r, 32<<10), max: maxSize}
}

func (d *Decoder) Read() (Message, error) {
	length, err := d.readHeader()
	if err != nil {
		return Message{}, err
	}
	if length > d.max {
		return Message{}, fmt.Errorf("%w: %d > %d bytes", ErrFrameTooLarge, length, d.max)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(d.r, body); err != nil {
		return Message{}, fmt.Errorf("corpo incompleto: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var message Message
	if err := decoder.Decode(&message); err != nil {
		return Message{}, fmt.Errorf("%w: JSON inválido: %v", ErrInvalidFrame, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Message{}, fmt.Errorf("%w: conteúdo depois do envelope", ErrInvalidFrame)
	}
	if message.Version <= 0 || message.Sequence == 0 || message.Method == "" {
		return Message{}, fmt.Errorf("%w: envelope incompleto", ErrInvalidFrame)
	}
	return message, nil
}

func (d *Decoder) readHeader() (int, error) {
	total := 0
	contentLength := -1
	for {
		line, err := d.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		total += len(line)
		if total > maxHeaderBytes {
			return 0, fmt.Errorf("%w: cabeçalho grande demais", ErrInvalidFrame)
		}
		if !strings.HasSuffix(line, "\r\n") {
			return 0, fmt.Errorf("%w: cabeçalho exige CRLF", ErrInvalidFrame)
		}
		line = strings.TrimSuffix(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return 0, fmt.Errorf("%w: header sem separador", ErrInvalidFrame)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if contentLength >= 0 {
			return 0, fmt.Errorf("%w: Content-Length duplicado", ErrInvalidFrame)
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%w: Content-Length inválido", ErrInvalidFrame)
		}
		contentLength = n
	}
	if contentLength < 0 {
		return 0, fmt.Errorf("%w: Content-Length ausente", ErrInvalidFrame)
	}
	return contentLength, nil
}
