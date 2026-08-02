package settings

import (
	"fmt"
	"strings"
	"sync"
)

// Manager é a porta de entrada consumida pela tela.
type Manager interface {
	All() ([]Value, error)
	Get(key Key) (Value, error)
	Set(key Key, value string) error
	Reset(key Key) error
	Info() []InfoRow
}

type Config struct {
	Store Store
	// Defaults sobrescreve o padrão declarado, para campos que só o
	// composition root conhece — o client_id gravado no build, por exemplo.
	Defaults map[Key]string
	// Lookup lê o ambiente. Injetada porque o core não importa os.
	Lookup func(name string) (string, bool)
	Rows   []InfoRow
}

// Service resolve a precedência e guarda o que o usuário mudou.
//
// O cache em memória existe porque os leitores síncronos — o resolvedor do
// client_id, a home decidindo se consulta a rede — rodam em caminhos que não
// podem tocar o disco a cada chamada.
type Service struct {
	config Config

	mutex  sync.RWMutex
	stored map[string]string
	fields map[Key]Field
	order  []Field
}

var _ Manager = (*Service)(nil)

// NewService carrega o que já está gravado. Um erro de leitura não impede a
// engine de abrir: a configuração volta aos padrões e a tela mostra a falha
// quando o usuário for gravar.
func NewService(config Config) (*Service, error) {
	if config.Lookup == nil {
		config.Lookup = func(string) (string, bool) { return "", false }
	}
	service := &Service{config: config, fields: map[Key]Field{}}
	for _, field := range Fields() {
		if override, ok := config.Defaults[field.Key]; ok {
			field.Default = override
		}
		service.fields[field.Key] = field
		service.order = append(service.order, field)
	}

	if config.Store == nil {
		service.stored = map[string]string{}
		return service, nil
	}
	stored, err := config.Store.Load()
	if stored == nil {
		stored = map[string]string{}
	}
	service.stored = stored
	return service, err
}

func (s *Service) All() ([]Value, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	values := make([]Value, 0, len(s.order))
	for _, field := range s.order {
		values = append(values, s.resolve(field))
	}
	return values, nil
}

func (s *Service) Get(key Key) (Value, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	field, ok := s.fields[key]
	if !ok {
		return Value{}, fmt.Errorf("%s: %w", key, ErrUnknownField)
	}
	return s.resolve(field), nil
}

// Set grava o valor do usuário. Valor vazio equivale a Reset: é o que o
// campo de texto produz quando alguém apaga tudo, e criar uma diferença
// entre "apagado" e "no padrão" só geraria estado invisível.
func (s *Service) Set(key Key, value string) error {
	field, ok := s.fields[key]
	if !ok {
		return fmt.Errorf("%s: %w", key, ErrUnknownField)
	}
	value = strings.TrimSpace(value)
	if field.Kind == KindToggle && value != "true" && value != "false" {
		return fmt.Errorf("%s aceita apenas true ou false", key)
	}
	if field.Validate != nil {
		if err := field.Validate(value); err != nil {
			return err
		}
	}
	if value == "" {
		return s.Reset(key)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	previous, existed := s.stored[string(key)]
	s.stored[string(key)] = value
	if err := s.persist(); err != nil {
		// Desfaz para que a memória não afirme algo que o disco não tem.
		if existed {
			s.stored[string(key)] = previous
		} else {
			delete(s.stored, string(key))
		}
		return err
	}
	return nil
}

func (s *Service) Reset(key Key) error {
	if _, ok := s.fields[key]; !ok {
		return fmt.Errorf("%s: %w", key, ErrUnknownField)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()

	previous, existed := s.stored[string(key)]
	if !existed {
		return nil
	}
	delete(s.stored, string(key))
	if err := s.persist(); err != nil {
		s.stored[string(key)] = previous
		return err
	}
	return nil
}

func (s *Service) Info() []InfoRow { return s.config.Rows }

// String é o leitor síncrono usado pela composição. Campo desconhecido
// devolve vazio em vez de entrar em pânico: uma configuração não pode
// derrubar a engine.
func (s *Service) String(key Key) string {
	value, err := s.Get(key)
	if err != nil {
		return ""
	}
	return value.Current
}

// Bool é o mesmo para interruptores.
func (s *Service) Bool(key Key) bool {
	value, err := s.Get(key)
	if err != nil {
		return false
	}
	return value.Bool()
}

// resolve aplica a precedência: o que o usuário gravou, depois o ambiente,
// depois o padrão. O usuário vem primeiro porque a tela mostra o valor dele;
// deixar o ambiente ganhar faria a engine usar um número diferente do que
// está escrito na tela.
func (s *Service) resolve(field Field) Value {
	if stored, ok := s.stored[string(field.Key)]; ok && stored != "" {
		return Value{Field: field, Current: stored, Source: SourceUser}
	}
	if field.EnvVar != "" {
		if fromEnv, ok := s.config.Lookup(field.EnvVar); ok && strings.TrimSpace(fromEnv) != "" {
			return Value{Field: field, Current: strings.TrimSpace(fromEnv), Source: SourceEnv}
		}
	}
	return Value{Field: field, Current: field.Default, Source: SourceDefault}
}

func (s *Service) persist() error {
	if s.config.Store == nil {
		return nil
	}
	snapshot := make(map[string]string, len(s.stored))
	for key, value := range s.stored {
		snapshot[key] = value
	}
	return s.config.Store.Save(snapshot)
}
