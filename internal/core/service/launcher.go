package service

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// UsageRecorder é o recorte do CatalogService de que o launcher depende.
// Declarar a interface aqui (no consumidor) evita acoplar os dois serviços.
type UsageRecorder interface {
	RecordRun(ctx context.Context, id domain.ToolID) error
}

// LauncherService implementa inbound.Launcher despachando para o ToolRunner
// capaz de atender o Kind da tool.
type LauncherService struct {
	repo     outbound.ToolRepository
	runners  []outbound.ToolRunner
	recorder UsageRecorder
	clock    outbound.Clock
	log      outbound.Logger

	mu       sync.RWMutex
	sessions map[domain.SessionID]*domain.Session
	cancels  map[domain.SessionID]context.CancelFunc
	seq      uint64
}

var _ inbound.Launcher = (*LauncherService)(nil)

// NewLauncher monta o serviço com o conjunto de runners disponíveis.
func NewLauncher(
	repo outbound.ToolRepository,
	recorder UsageRecorder,
	clock outbound.Clock,
	log outbound.Logger,
	runners ...outbound.ToolRunner,
) *LauncherService {
	if clock == nil {
		clock = outbound.SystemClock
	}
	return &LauncherService{
		repo:     repo,
		runners:  runners,
		recorder: recorder,
		clock:    clock,
		log:      log,
		sessions: make(map[domain.SessionID]*domain.Session),
		cancels:  make(map[domain.SessionID]context.CancelFunc),
	}
}

// Launch implementa inbound.Launcher.
//
// A política de risco vive aqui, e não na TUI: qualquer driving adapter que
// chame Launch em uma tool destrutiva sem confirmação recebe o mesmo erro.
func (s *LauncherService) Launch(
	ctx context.Context,
	id domain.ToolID,
	args domain.Args,
	opts inbound.LaunchOptions,
) (domain.Session, error) {
	tool, err := s.repo.ByID(ctx, id)
	if err != nil {
		return domain.Session{}, err
	}
	if tool.Risk.NeedsConfirmation() && !opts.Confirmed {
		return domain.Session{}, domain.WrapTool(id, domain.ErrConfirmationRequired)
	}

	runner := s.runnerFor(tool.Kind)
	if runner == nil {
		return domain.Session{}, domain.WrapTool(id, fmt.Errorf("nenhum runner para kind %s", tool.Kind))
	}

	// O contexto da execução é desacoplado do contexto da chamada: a TUI
	// pode seguir redesenhando (ou até fechar o frame atual) sem matar o
	// processo em andamento.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	sess := domain.Session{
		ID:      s.nextID(tool.ID),
		ToolID:  tool.ID,
		Args:    args,
		Phase:   domain.PhasePending,
		Started: s.clock.Now(),
	}

	s.mu.Lock()
	s.sessions[sess.ID] = &sess
	s.cancels[sess.ID] = cancel
	s.mu.Unlock()

	updates, err := runner.Run(runCtx, tool, args)
	if err != nil {
		cancel()
		s.finish(sess.ID, domain.PhaseFailed, err)
		return sess, domain.WrapTool(id, err)
	}

	if s.recorder != nil {
		if err := s.recorder.RecordRun(ctx, tool.ID); err != nil && s.log != nil {
			// Falhar ao contabilizar não pode abortar a execução.
			s.log.Warn("falha ao registrar uso", "tool", tool.ID, "err", err)
		}
	}

	go s.consume(sess.ID, cancel, updates)
	return sess, nil
}

// consume drena as transições reportadas pelo runner até o canal fechar.
func (s *LauncherService) consume(id domain.SessionID, cancel context.CancelFunc, updates <-chan domain.Session) {
	defer cancel()
	for u := range updates {
		s.mu.Lock()
		if cur, ok := s.sessions[id]; ok {
			cur.Phase = u.Phase
			cur.ExitCode = u.ExitCode
			cur.Err = u.Err
			if u.Phase.Terminal() {
				cur.Finished = s.clock.Now()
			}
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	if cur, ok := s.sessions[id]; ok && !cur.Phase.Terminal() {
		// Canal fechou sem fase final: o runner morreu sem reportar.
		cur.Phase = domain.PhaseSucceeded
		cur.Finished = s.clock.Now()
	}
	delete(s.cancels, id)
	s.mu.Unlock()
}

// Cancel implementa inbound.Launcher.
func (s *LauncherService) Cancel(_ context.Context, id domain.SessionID) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	if sess, exists := s.sessions[id]; exists && ok {
		sess.Phase = domain.PhaseCanceled
		sess.Finished = s.clock.Now()
	}
	delete(s.cancels, id)
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("sessão %s: %w", id, domain.ErrCanceled)
	}
	cancel()
	return nil
}

// Sessions implementa inbound.Launcher, devolvendo cópias ordenadas por início
// decrescente.
func (s *LauncherService) Sessions(_ context.Context) ([]domain.Session, error) {
	s.mu.RLock()
	out := make([]domain.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	s.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, nil
}

// finish marca uma sessão como terminal.
func (s *LauncherService) finish(id domain.SessionID, phase domain.Phase, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.Phase = phase
		sess.Err = err
		sess.Finished = s.clock.Now()
	}
	delete(s.cancels, id)
}

// runnerFor escolhe o primeiro runner que declara suportar o Kind.
func (s *LauncherService) runnerFor(kind domain.Kind) outbound.ToolRunner {
	for _, r := range s.runners {
		if r.Supports(kind) {
			return r
		}
	}
	return nil
}

// nextID gera um identificador de sessão legível e único no processo.
func (s *LauncherService) nextID(tool domain.ToolID) domain.SessionID {
	s.mu.Lock()
	s.seq++
	n := s.seq
	s.mu.Unlock()
	return domain.SessionID(fmt.Sprintf("%s#%d", tool, n))
}
