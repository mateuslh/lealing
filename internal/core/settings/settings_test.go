package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/settings"
)

// memoryStore troca o disco por um mapa e sabe falhar sob demanda.
type memoryStore struct {
	values  map[string]string
	saves   int
	saveErr error
	loadErr error
}

func (m *memoryStore) Load() (map[string]string, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	copied := make(map[string]string, len(m.values))
	for key, value := range m.values {
		copied[key] = value
	}
	return copied, nil
}

func (m *memoryStore) Save(values map[string]string) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	m.values = values
	return nil
}

func newService(t *testing.T, store settings.Store, env map[string]string) *settings.Service {
	t.Helper()
	service, err := settings.NewService(settings.Config{
		Store: store,
		Defaults: map[settings.Key]string{
			settings.KeyGitHubClientID: "Ov23liDoBuild",
		},
		Lookup: func(name string) (string, bool) {
			value, ok := env[name]
			return value, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestPrecedenciaUsuarioAmbientePadrao(t *testing.T) {
	// Só o padrão.
	service := newService(t, &memoryStore{}, nil)
	value, err := service.Get(settings.KeyGitHubClientID)
	if err != nil {
		t.Fatal(err)
	}
	if value.Current != "Ov23liDoBuild" || value.Source != settings.SourceDefault {
		t.Fatalf("padrão = %+v", value)
	}

	// O ambiente entra na frente do padrão.
	service = newService(t, &memoryStore{}, map[string]string{"LEALING_GITHUB_CLIENT_ID": "Ov23liDoAmbiente"})
	value, _ = service.Get(settings.KeyGitHubClientID)
	if value.Current != "Ov23liDoAmbiente" || value.Source != settings.SourceEnv {
		t.Fatalf("ambiente = %+v", value)
	}

	// E o usuário vence o ambiente: a tela mostra o valor dele, e a engine
	// não pode usar outro.
	if err := service.Set(settings.KeyGitHubClientID, "Ov23liDoUsuario"); err != nil {
		t.Fatal(err)
	}
	value, _ = service.Get(settings.KeyGitHubClientID)
	if value.Current != "Ov23liDoUsuario" || value.Source != settings.SourceUser {
		t.Fatalf("usuário = %+v", value)
	}
}

// Só o que difere do padrão vai para o disco: gravar a configuração inteira
// congelaria padrões que a engine deve poder melhorar.
func TestApenasOAlteradoEhPersistido(t *testing.T) {
	store := &memoryStore{}
	service := newService(t, store, nil)

	if err := service.Set(settings.KeyGreetingName, "Chefia"); err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 1 || store.values["home.greeting_name"] != "Chefia" {
		t.Fatalf("gravado = %+v", store.values)
	}
}

func TestResetVoltaAoPadraoESaiDoDisco(t *testing.T) {
	store := &memoryStore{values: map[string]string{"home.greeting_name": "Chefia"}}
	service := newService(t, store, nil)

	if err := service.Reset(settings.KeyGreetingName); err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 0 {
		t.Fatalf("o valor sobreviveu ao reset: %+v", store.values)
	}
	value, _ := service.Get(settings.KeyGreetingName)
	if value.Source != settings.SourceDefault {
		t.Fatalf("origem após reset = %v", value.Source)
	}
}

// Apagar tudo no campo de texto é o gesto natural de "quero o padrão".
func TestValorVazioEquivaleAReset(t *testing.T) {
	store := &memoryStore{values: map[string]string{"home.greeting_name": "Chefia"}}
	service := newService(t, store, nil)

	if err := service.Set(settings.KeyGreetingName, "   "); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.values["home.greeting_name"]; ok {
		t.Fatalf("valor vazio virou ajuste: %+v", store.values)
	}
}

func TestValidacaoRecusaValorInvalidoSemGravar(t *testing.T) {
	store := &memoryStore{}
	service := newService(t, store, nil)

	for name, pair := range map[string]struct {
		key   settings.Key
		value string
	}{
		"url sem https":     {settings.KeyMarketplaceIndex, "http://exemplo.test/i.json"},
		"url sem esquema":   {settings.KeyMarketplaceIndex, "exemplo.test"},
		"client id com /":   {settings.KeyGitHubClientID, "Ov23li/quebrado"},
		"saudação com nova": {settings.KeyGreetingName, "linha\nquebrada"},
		"toggle inválido":   {settings.KeyMarketplaceOnHome, "talvez"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := service.Set(pair.key, pair.value); err == nil {
				t.Fatal("valor inválido foi aceito")
			}
		})
	}
	if store.saves != 0 {
		t.Fatalf("houve %d gravações apesar das recusas", store.saves)
	}
}

// Se o disco recusa, a memória não pode afirmar o que não foi gravado.
func TestFalhaAoGravarNaoDeixaValorFantasma(t *testing.T) {
	store := &memoryStore{saveErr: errors.New("disco cheio")}
	service := newService(t, store, nil)

	if err := service.Set(settings.KeyGreetingName, "Chefia"); err == nil {
		t.Fatal("Set escondeu a falha do disco")
	}
	value, _ := service.Get(settings.KeyGreetingName)
	if value.Source == settings.SourceUser {
		t.Fatalf("a memória guardou um valor que o disco recusou: %+v", value)
	}
}

// A engine precisa abrir mesmo com a configuração ilegível.
func TestLeituraQuebradaNaoImpedeAConfiguracaoDeExistir(t *testing.T) {
	service, err := settings.NewService(settings.Config{
		Store: &memoryStore{loadErr: errors.New("json inválido")},
	})
	if err == nil {
		t.Fatal("NewService escondeu a falha de leitura")
	}
	if service == nil {
		t.Fatal("NewService devolveu nil; a engine não teria configuração nenhuma")
	}
	values, listErr := service.All()
	if listErr != nil || len(values) == 0 {
		t.Fatalf("All = %v (%d valores)", listErr, len(values))
	}
}

func TestCampoDesconhecidoEhRecusado(t *testing.T) {
	service := newService(t, &memoryStore{}, nil)
	if err := service.Set("inexistente", "x"); !errors.Is(err, settings.ErrUnknownField) {
		t.Fatalf("Set = %v", err)
	}
	if _, err := service.Get("inexistente"); !errors.Is(err, settings.ErrUnknownField) {
		t.Fatalf("Get = %v", err)
	}
	// O leitor síncrono não pode derrubar a engine por um campo que sumiu.
	if service.String("inexistente") != "" || service.Bool("inexistente") {
		t.Fatal("leitor síncrono inventou valor para campo desconhecido")
	}
}

func TestTodoCampoDeclaradoTemSecaoConhecida(t *testing.T) {
	sections := map[string]bool{}
	for _, section := range settings.Sections() {
		sections[section.ID] = true
	}
	for _, field := range settings.Fields() {
		if !sections[field.Section] {
			t.Errorf("campo %s aponta para a seção inexistente %q", field.Key, field.Section)
		}
		if strings.TrimSpace(field.Label) == "" || strings.TrimSpace(field.Description) == "" {
			t.Errorf("campo %s sem rótulo ou descrição", field.Key)
		}
	}
}

func TestInterruptorLeComoBooleano(t *testing.T) {
	service := newService(t, &memoryStore{}, nil)
	if !service.Bool(settings.KeyMarketplaceOnHome) {
		t.Fatal("o padrão do interruptor deveria estar ligado")
	}
	if err := service.Set(settings.KeyMarketplaceOnHome, "false"); err != nil {
		t.Fatal(err)
	}
	if service.Bool(settings.KeyMarketplaceOnHome) {
		t.Fatal("o interruptor não desligou")
	}
}
