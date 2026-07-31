// Package interactive contém o caso de uso de sessões screen-v1. Os tipos
// daqui são puros e independem do protocolo em fio, de processo e de TUI.
package interactive

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mateuslh/lealing/internal/core/domain"
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
)

const (
	CapabilityNavigationBack      = "navigation.back"
	CapabilityNotificationShow    = "notification.show"
	CapabilityClipboardWrite      = "clipboard.write"
	CapabilityConfirmationRequest = "confirmation.request"
	CapabilityBrowserOpen         = "browser.open"
)

type Frame struct {
	Width  int
	Height int
}

type Theme struct {
	Primary, Secondary, Accent string
	Success, Warning, Danger   string
	Text, Muted, Faint         string
	Border, Surface            string
}

type EventKind string

const (
	EventKey          EventKind = "key"
	EventPaste        EventKind = "paste"
	EventMouse        EventKind = "mouse"
	EventResize       EventKind = "resize"
	EventThemeChanged EventKind = "themeChanged"
	EventFocus        EventKind = "focus"
	EventBlur         EventKind = "blur"
	EventTick         EventKind = "tick"
	EventShutdown     EventKind = "shutdown"
)

type Event struct {
	Kind         EventKind
	Key          *KeyEvent
	Paste        *PasteEvent
	Mouse        *MouseEvent
	Resize       *ResizeEvent
	ThemeChanged *ThemeChangedEvent
	Tick         *TickEvent
	Shutdown     *ShutdownEvent
}

type KeyEvent struct {
	Code, Text                 string
	Ctrl, Alt, Shift, Repeated bool
}

type PasteEvent struct{ Text string }

type MouseEvent struct {
	X, Y             int
	Button, Action   string
	Ctrl, Alt, Shift bool
}

type ResizeEvent struct{ Frame Frame }
type ThemeChangedEvent struct{ Theme Theme }
type TickEvent struct{ UnixMilli int64 }
type ShutdownEvent struct{ Reason string }

type Hint struct {
	Key, Label string
}

type Cursor struct {
	X, Y    int
	Visible bool
	Shape   string
}

type Snapshot struct {
	Sequence  uint64
	Body      string
	Hints     []Hint
	Status    string
	Capturing bool
	Cursor    *Cursor
}

type HostRequest struct {
	ID, Method string
	Params     json.RawMessage
}

type HostResponse struct {
	ID     string
	Result json.RawMessage
	Error  *ProtocolError
}

type ProtocolError struct {
	Code, Message string
	Fatal         bool
}

// Update é uma mudança observável da sessão. Snapshot, HostRequest e Err são
// opcionais; State sempre informa o estágio depois da mudança.
type Update struct {
	State       State
	Snapshot    *Snapshot
	HostRequest *HostRequest
	Err         error
}

type OpenOptions struct {
	Frame Frame
	Theme Theme
}

type StartOptions struct {
	EngineVersion string
	Platform      string
	Architecture  string
	Frame         Frame
	Theme         Theme
	DataDir       string
	CacheDir      string
	Capabilities  []string
	Permissions   domain.ToolPermissions
}

// Session é o lado vivo da porta de saída. Seus métodos podem fazer I/O e,
// portanto, só são chamados pela TUI dentro de comandos assíncronos.
type Session interface {
	Updates() <-chan Update
	Send(ctx context.Context, event Event) error
	Respond(ctx context.Context, response HostResponse) error
	Shutdown(ctx context.Context) error
}

// Runtime é a porta de saída implementada pelo gerenciador de subprocessos.
type Runtime interface {
	Start(ctx context.Context, tool domain.Tool, options StartOptions) (Session, error)
}

// Catalog é o recorte do repositório que o serviço precisa.
type Catalog interface {
	ByID(ctx context.Context, id domain.ToolID) (domain.Tool, error)
}

// Opener é a porta de entrada consumida pela PluginScreen genérica.
type Opener interface {
	Open(ctx context.Context, id domain.ToolID, options OpenOptions) (Session, error)
}

type ServiceConfig struct {
	EngineVersion string
	Platform      string
	Architecture  string
	DataRoot      string
	CacheRoot     string
	UserHome      string
	Capabilities  []string
}

type Service struct {
	repo    Catalog
	runtime Runtime
	config  ServiceConfig
}

var _ Opener = (*Service)(nil)

func NewService(repo Catalog, runtime Runtime, config ServiceConfig) *Service {
	return &Service{repo: repo, runtime: runtime, config: config}
}

func (s *Service) Open(ctx context.Context, id domain.ToolID, options OpenOptions) (Session, error) {
	tool, err := s.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !tool.Interactive() {
		return nil, domain.WrapTool(id, fmt.Errorf("runtime não usa screen-v1"))
	}
	capabilities := intersection(tool.Runtime.Capabilities, s.config.Capabilities)
	return s.runtime.Start(ctx, tool, StartOptions{
		EngineVersion: s.config.EngineVersion,
		Platform:      s.config.Platform, Architecture: s.config.Architecture,
		Frame: options.Frame, Theme: options.Theme,
		DataDir:      filepath.Join(s.config.DataRoot, string(id)),
		CacheDir:     filepath.Join(s.config.CacheRoot, string(id)),
		Capabilities: capabilities,
		Permissions:  resolvePermissions(tool.Runtime.Permissions, s.config.UserHome),
	})
}

func resolvePermissions(permissions domain.ToolPermissions, home string) domain.ToolPermissions {
	expand := func(paths []string) []string {
		result := make([]string, len(paths))
		for index, path := range paths {
			if strings.HasPrefix(filepath.ToSlash(path), "~/") && home != "" {
				path = filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(path), "~/")))
			}
			result[index] = path
		}
		return result
	}
	return domain.ToolPermissions{
		ReadPaths: expand(permissions.ReadPaths), WritePaths: expand(permissions.WritePaths),
		Network: permissions.Network, Subprocess: permissions.Subprocess,
	}
}

func intersection(requested, offered []string) []string {
	allowed := make(map[string]bool, len(offered))
	for _, capability := range offered {
		allowed[capability] = true
	}
	result := make([]string, 0, len(requested))
	for _, capability := range requested {
		if allowed[capability] {
			result = append(result, capability)
		}
	}
	return result
}

// IncompatibleError preserva os dois intervalos para uma mensagem humana.
type IncompatibleError struct {
	EngineMin, EngineMax int
	ToolMin, ToolMax     int
}

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf(
		"protocolo incompatível: engine suporta %d–%d; tool suporta %d–%d",
		e.EngineMin, e.EngineMax, e.ToolMin, e.ToolMax,
	)
}
