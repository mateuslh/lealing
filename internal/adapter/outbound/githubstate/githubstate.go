// Package githubstate guarda o documento de preferências em um repositório
// privado do próprio usuário, pela Contents API.
//
// A API de conteúdo, e não git, porque a engine não pode assumir git
// instalado nem clonar um repositório inteiro para escrever um arquivo — e
// porque ela já oferece o controle de concorrência que o núcleo precisa: a
// escrita carrega o SHA do arquivo que se pretende substituir, e o GitHub
// recusa se ele não for mais o atual.
package githubstate

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/usersync"
)

const (
	defaultAPI    = "https://api.github.com"
	apiVersion    = "2022-11-28"
	responseLimit = 8 << 20
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type Config struct {
	Client HTTPClient
	// BaseURL é sobrescrita em teste e por instalações GitHub Enterprise.
	BaseURL string
	// Description e Path descrevem o repositório criado e onde o documento
	// mora dentro dele.
	Description string
	Path        string
}

type Store struct{ config Config }

var _ usersync.Remote = (*Store)(nil)

func New(config Config) *Store {
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultAPI
	}
	if config.Path == "" {
		config.Path = usersync.RemotePath
	}
	if config.Description == "" {
		config.Description = "Preferências do lealing (privado, gerado pela engine)"
	}
	return &Store{config: config}
}

type repositoryPayload struct {
	Name    string                 `json:"name"`
	Owner   struct{ Login string } `json:"owner"`
	Private bool                   `json:"private"`
	HTMLURL string                 `json:"html_url"`
}

// Ensure cria o repositório privado na primeira vez e, nas seguintes, apenas
// confere que ele continua privado — um estado que o usuário pode ter mudado
// no site sem perceber o que isso significa para as preferências dele.
func (s *Store) Ensure(ctx context.Context, credential usersync.Credential, name string) (usersync.Repository, error) {
	login, err := s.login(ctx, credential)
	if err != nil {
		return usersync.Repository{}, err
	}

	existing, status, err := s.repository(ctx, credential, login, name)
	switch {
	case err != nil:
		return usersync.Repository{}, err
	case status == http.StatusOK:
		repository := usersync.Repository{
			Owner: existing.Owner.Login, Name: existing.Name,
			Private: existing.Private, URL: existing.HTMLURL,
		}
		if !existing.Private {
			return repository, fmt.Errorf(
				"o repositório %s/%s é público; deixe-o privado antes de sincronizar preferências", login, name)
		}
		return repository, nil
	case status != http.StatusNotFound:
		return usersync.Repository{}, fmt.Errorf("GitHub respondeu %d ao consultar o repositório", status)
	}

	created, err := s.createRepository(ctx, credential, name)
	if err != nil {
		return usersync.Repository{}, err
	}
	return usersync.Repository{
		Owner: created.Owner.Login, Name: created.Name,
		Private: created.Private, URL: created.HTMLURL, Created: true,
	}, nil
}

type contentPayload struct {
	Content  string `json:"content"`
	SHA      string `json:"sha"`
	Encoding string `json:"encoding"`
	Message  string `json:"message"`
}

func (s *Store) Read(ctx context.Context, credential usersync.Credential, name string) (usersync.Snapshot, error) {
	login, err := s.login(ctx, credential)
	if err != nil {
		return usersync.Snapshot{}, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		s.config.BaseURL, login, name, s.config.Path)

	raw, status, err := s.do(ctx, credential, http.MethodGet, endpoint, nil)
	if err != nil {
		return usersync.Snapshot{}, err
	}
	// Repositório recém-criado não tem o arquivo, e isso não é erro: é o
	// primeiro envio ainda não feito.
	if status == http.StatusNotFound {
		return usersync.Snapshot{Missing: true}, nil
	}
	if status != http.StatusOK {
		return usersync.Snapshot{}, fmt.Errorf("GitHub respondeu %d ao ler o estado", status)
	}

	var payload contentPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return usersync.Snapshot{}, err
	}
	if payload.Encoding != "base64" && payload.Encoding != "" {
		return usersync.Snapshot{}, fmt.Errorf("codificação inesperada no estado remoto: %s", payload.Encoding)
	}
	// A API quebra o base64 em linhas; o decoder padrão não as aceita.
	decoded, err := base64.StdEncoding.DecodeString(
		strings.NewReplacer("\n", "", "\r", "").Replace(payload.Content))
	if err != nil {
		return usersync.Snapshot{}, fmt.Errorf("estado remoto ilegível: %w", err)
	}

	var state usersync.State
	if err := json.Unmarshal(decoded, &state); err != nil {
		return usersync.Snapshot{}, fmt.Errorf("JSON do estado remoto inválido: %w", err)
	}
	return usersync.Snapshot{State: state, Revision: payload.SHA}, nil
}

func (s *Store) Write(
	ctx context.Context,
	credential usersync.Credential,
	name string,
	state usersync.State,
	expected string,
) (string, error) {
	login, err := s.login(ctx, credential)
	if err != nil {
		return "", err
	}
	document, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}
	document = append(document, '\n')

	// A revisão atual é sempre relida: ela é o que o GitHub compara. Sem
	// informá-la, a API recusa a substituição de um arquivo existente.
	current, err := s.Read(ctx, credential, name)
	if err != nil {
		return "", err
	}
	if expected != "" && !current.Missing && current.Revision != expected {
		return "", usersync.ErrConflict
	}

	body := map[string]any{
		"message": fmt.Sprintf("lealing: preferências de %s", state.Device),
		"content": base64.StdEncoding.EncodeToString(document),
	}
	if !current.Missing {
		body["sha"] = current.Revision
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		s.config.BaseURL, login, name, s.config.Path)

	raw, status, err := s.do(ctx, credential, http.MethodPut, endpoint, body)
	if err != nil {
		return "", err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return "", usersync.ErrConflict
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("GitHub respondeu %d ao gravar o estado", status)
	}

	var payload struct {
		Content struct {
			SHA string `json:"sha"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	return payload.Content.SHA, nil
}

// login descobre o dono da conta. É consultado a cada operação porque o token
// pode ter sido emitido para outra conta desde a última vez.
func (s *Store) login(ctx context.Context, credential usersync.Credential) (string, error) {
	raw, status, err := s.do(ctx, credential, http.MethodGet, s.config.BaseURL+"/user", nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnauthorized {
		return "", usersync.ErrNotAuthenticated
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("GitHub respondeu %d ao identificar a conta", status)
	}
	var parsed struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Login == "" {
		return "", errors.New("o GitHub não devolveu o login da conta")
	}
	return parsed.Login, nil
}

func (s *Store) repository(
	ctx context.Context, credential usersync.Credential, login, name string,
) (repositoryPayload, int, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s", s.config.BaseURL, login, name)
	raw, status, err := s.do(ctx, credential, http.MethodGet, endpoint, nil)
	if err != nil || status != http.StatusOK {
		return repositoryPayload{}, status, err
	}
	var payload repositoryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return repositoryPayload{}, status, err
	}
	return payload, status, nil
}

func (s *Store) createRepository(
	ctx context.Context, credential usersync.Credential, name string,
) (repositoryPayload, error) {
	raw, status, err := s.do(ctx, credential, http.MethodPost, s.config.BaseURL+"/user/repos", map[string]any{
		"name": name, "private": true, "description": s.config.Description,
		"auto_init": true, "has_issues": false, "has_projects": false, "has_wiki": false,
	})
	if err != nil {
		return repositoryPayload{}, err
	}
	if status != http.StatusCreated {
		return repositoryPayload{}, fmt.Errorf("GitHub respondeu %d ao criar o repositório %s", status, name)
	}
	var payload repositoryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return repositoryPayload{}, err
	}
	if !payload.Private {
		return payload, errors.New("o repositório foi criado público; interrompendo antes de gravar preferências")
	}
	return payload, nil
}

func (s *Store) do(
	ctx context.Context, credential usersync.Credential, method, endpoint string, body any,
) ([]byte, int, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := s.config.Client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return nil, response.StatusCode, err
	}
	return raw, response.StatusCode, nil
}
