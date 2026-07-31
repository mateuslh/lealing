package protocol_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/sdk/protocol"
)

func benchmarkFrame(b *testing.B, width, height int) {
	body := strings.Repeat(strings.Repeat("x", width)+"\n", height)
	m, err := protocol.NewMessage(1, 1, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 1, Body: body})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		var stream bytes.Buffer
		if err := protocol.NewEncoder(&stream).Write(m); err != nil {
			b.Fatal(err)
		}
		if _, err := protocol.NewDecoder(&stream).Read(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFrame60x20(b *testing.B)  { benchmarkFrame(b, 60, 20) }
func BenchmarkFrame150x42(b *testing.B) { benchmarkFrame(b, 150, 42) }

func BenchmarkMemoriaProtocolo(b *testing.B) {
	body := strings.Repeat("\x1b[38;2;122;162;247m█\x1b[0m", 20_000)
	message, err := protocol.NewMessage(1, 1, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 1, Body: body})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		var stream bytes.Buffer
		if err := protocol.NewEncoder(&stream).Write(message); err != nil {
			b.Fatal(err)
		}
		if _, err := protocol.NewDecoder(&stream).Read(); err != nil {
			b.Fatal(err)
		}
	}
}
