// Package protocol declara o contrato serializável entre a engine e tools
// screen-v1. Este pacote usa somente a biblioteca padrão: nenhuma decisão de
// terminal ou framework atravessa a fronteira entre processos.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	// Version1 é a primeira versão pública do protocolo.
	Version1 = 1
	// UIModeScreenV1 entrega à engine um frame completo para a área central.
	UIModeScreenV1 = "screen-v1"
	// MaxMessageSize limita memória e tempo gastos em uma mensagem individual.
	MaxMessageSize = 8 << 20
	// MaxFrameWidth e MaxFrameHeight impedem snapshots de induzir alocações
	// desproporcionais mesmo quando o terminal reporta geometria corrompida.
	MaxFrameWidth  = 500
	MaxFrameHeight = 200
)

// Métodos do protocolo v1.
const (
	MethodInitialize   = "initialize"
	MethodInitialized  = "initialized"
	MethodUIEvent      = "ui/event"
	MethodUISnapshot   = "ui/snapshot"
	MethodHostRequest  = "host/request"
	MethodHostResponse = "host/response"
	MethodShutdown     = "shutdown"
	MethodError        = "error"
)

// Capacidades que uma tool pode negociar com a engine.
const (
	CapabilityNavigationBack      = "navigation.back"
	CapabilityNotificationShow    = "notification.show"
	CapabilityClipboardWrite      = "clipboard.write"
	CapabilityConfirmationRequest = "confirmation.request"
	CapabilityBrowserOpen         = "browser.open"
)

// Message é o envelope comum. Payload permanece JSON para que métodos novos
// possam ser ignorados ou encaminhados sem acoplar o framing a cada DTO.
type Message struct {
	Version  int             `json:"version"`
	Sequence uint64          `json:"sequence"`
	Method   string          `json:"method"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// NewMessage serializa um payload dentro de um envelope validado.
func NewMessage(version int, sequence uint64, method string, payload any) (Message, error) {
	if version <= 0 {
		return Message{}, errors.New("versão do protocolo deve ser positiva")
	}
	if sequence == 0 {
		return Message{}, errors.New("sequência deve começar em 1")
	}
	if method == "" {
		return Message{}, errors.New("método do protocolo é obrigatório")
	}
	var raw json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Message{}, fmt.Errorf("serializar %s: %w", method, err)
		}
		raw = encoded
	}
	return Message{Version: version, Sequence: sequence, Method: method, Payload: raw}, nil
}

// DecodePayload decodifica o payload de forma estrita quanto ao JSON.
func DecodePayload[T any](message Message) (T, error) {
	var out T
	if len(message.Payload) == 0 {
		return out, fmt.Errorf("%s sem payload", message.Method)
	}
	if err := json.Unmarshal(message.Payload, &out); err != nil {
		return out, fmt.Errorf("payload de %s: %w", message.Method, err)
	}
	return out, nil
}

// VersionRange é um intervalo inclusivo de versões suportadas.
type VersionRange struct {
	Min int `json:"min" yaml:"min"`
	Max int `json:"max" yaml:"max"`
}

// Valid informa se o intervalo é utilizável.
func (r VersionRange) Valid() bool { return r.Min > 0 && r.Max >= r.Min }

// Negotiate escolhe a versão mais alta comum aos dois lados.
func Negotiate(host, tool VersionRange) (int, bool) {
	if !host.Valid() || !tool.Valid() {
		return 0, false
	}
	low := max(host.Min, tool.Min)
	high := min(host.Max, tool.Max)
	if low > high {
		return 0, false
	}
	return high, true
}

// Frame é a área central disponível, já sem o chrome da engine.
type Frame struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Clamp aplica os limites públicos da v1.
func (f Frame) Clamp() Frame {
	f.Width = min(max(f.Width, 1), MaxFrameWidth)
	f.Height = min(max(f.Height, 1), MaxFrameHeight)
	return f
}

// Theme é a paleta serializada oferecida pela engine. Cores usam a notação
// ANSI aceita pelo Lip Gloss (hexadecimal RGB ou índice), mas continuam texto
// no protocolo.
type Theme struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Accent    string `json:"accent"`
	Success   string `json:"success"`
	Warning   string `json:"warning"`
	Danger    string `json:"danger"`
	Text      string `json:"text"`
	Muted     string `json:"muted"`
	Faint     string `json:"faint"`
	Border    string `json:"border"`
	Surface   string `json:"surface"`
}

// Permissions são concessões concretas para esta instalação. Elas informam
// e limitam a tool; não substituem isolamento do sistema operacional.
type Permissions struct {
	Filesystem FilesystemPermissions `json:"filesystem"`
	Network    bool                  `json:"network"`
	Subprocess bool                  `json:"subprocess"`
}

type FilesystemPermissions struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

// Initialize é enviado pela engine logo após o spawn.
type Initialize struct {
	Protocol      VersionRange `json:"protocol"`
	ToolID        string       `json:"toolId"`
	EngineVersion string       `json:"engineVersion"`
	Platform      string       `json:"platform"`
	Architecture  string       `json:"architecture"`
	Frame         Frame        `json:"frame"`
	Theme         Theme        `json:"theme"`
	HomeDir       string       `json:"homeDir,omitempty"`
	DataDir       string       `json:"dataDir"`
	CacheDir      string       `json:"cacheDir"`
	Capabilities  []string     `json:"capabilities,omitempty"`
	Permissions   Permissions  `json:"permissions"`
}

// Initialized conclui o handshake do lado da tool.
type Initialized struct {
	Protocol     VersionRange `json:"protocol"`
	ToolVersion  string       `json:"toolVersion"`
	Capabilities []string     `json:"capabilities,omitempty"`
	UIMode       string       `json:"uiMode"`
	State        string       `json:"state"`
	Snapshot     *Snapshot    `json:"snapshot,omitempty"`
}

// Tipos de evento normalizado.
const (
	EventKey          = "key"
	EventPaste        = "paste"
	EventMouse        = "mouse"
	EventResize       = "resize"
	EventThemeChanged = "themeChanged"
	EventFocus        = "focus"
	EventBlur         = "blur"
	EventTick         = "tick"
	EventShutdown     = "shutdown"
)

// UIEvent carrega exatamente um dos eventos abaixo, identificado por Type.
type UIEvent struct {
	Type         string             `json:"type"`
	Key          *KeyEvent          `json:"key,omitempty"`
	Paste        *PasteEvent        `json:"paste,omitempty"`
	Mouse        *MouseEvent        `json:"mouse,omitempty"`
	Resize       *ResizeEvent       `json:"resize,omitempty"`
	ThemeChanged *ThemeChangedEvent `json:"themeChanged,omitempty"`
	Focus        *FocusEvent        `json:"focus,omitempty"`
	Blur         *BlurEvent         `json:"blur,omitempty"`
	Tick         *TickEvent         `json:"tick,omitempty"`
	Shutdown     *ShutdownEvent     `json:"shutdown,omitempty"`
}

type KeyEvent struct {
	Code     string `json:"code"`
	Text     string `json:"text,omitempty"`
	Ctrl     bool   `json:"ctrl,omitempty"`
	Alt      bool   `json:"alt,omitempty"`
	Shift    bool   `json:"shift,omitempty"`
	Repeated bool   `json:"repeated,omitempty"`
}

type PasteEvent struct {
	Text string `json:"text"`
}

type MouseEvent struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button string `json:"button"`
	Action string `json:"action"`
	Ctrl   bool   `json:"ctrl,omitempty"`
	Alt    bool   `json:"alt,omitempty"`
	Shift  bool   `json:"shift,omitempty"`
}

type ResizeEvent struct {
	Frame Frame `json:"frame"`
}

type ThemeChangedEvent struct {
	Theme Theme `json:"theme"`
}

type FocusEvent struct{}
type BlurEvent struct{}

type TickEvent struct {
	UnixMilli int64 `json:"unixMilli"`
}

type ShutdownEvent struct {
	Reason string `json:"reason,omitempty"`
}

// Hint alimenta a statusbar da engine.
type Hint struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Cursor descreve uma posição relativa ao Body, nunca ao terminal inteiro.
type Cursor struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Visible bool   `json:"visible"`
	Shape   string `json:"shape,omitempty"`
}

// Snapshot é o frame completo mais recente da tool.
type Snapshot struct {
	Sequence  uint64  `json:"sequence"`
	Body      string  `json:"body"`
	Hints     []Hint  `json:"hints,omitempty"`
	Status    string  `json:"status,omitempty"`
	Capturing bool    `json:"capturing,omitempty"`
	Cursor    *Cursor `json:"cursor,omitempty"`
}

// HostRequest pede à engine uma ação que pertence ao host.
type HostRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// HostResponse conclui uma HostRequest.
type HostResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type NotificationParams struct {
	Message string `json:"message"`
	Level   string `json:"level,omitempty"`
}

type ClipboardParams struct {
	Text string `json:"text"`
}

type ConfirmationParams struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	ConfirmLabel string `json:"confirmLabel,omitempty"`
	CancelLabel  string `json:"cancelLabel,omitempty"`
}

type ConfirmationResult struct {
	Confirmed bool `json:"confirmed"`
}

type BrowserParams struct {
	URL string `json:"url"`
}

// Shutdown inicia o encerramento gracioso.
type Shutdown struct {
	Reason string `json:"reason,omitempty"`
}

// Error é serializável e não expõe tipos internos de nenhum processo.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal,omitempty"`
}
