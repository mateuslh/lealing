// Package githubauth implementa o OAuth Device Flow do GitHub.
//
// O device flow existe justamente para programas de terminal: não há segredo
// de cliente embutido no binário — que qualquer um extrairia —, o usuário
// aprova em um browser já autenticado e o token volta pelo polling.
package githubauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/usersync"
)

const (
	deviceURL   = "https://github.com/login/device/code"
	tokenURL    = "https://github.com/login/oauth/access_token"
	identityURL = "https://api.github.com/user"

	// scope é o mínimo que permite criar e escrever num repositório privado.
	// "repo" cobre público e privado; o GitHub não oferece um escopo só de
	// privados em OAuth App.
	scope = "repo"

	responseLimit = 1 << 20
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type Config struct {
	Client HTTPClient
	// ClientID é o OAuth App registrado pelo mantenedor. Sem ele o recurso
	// fica desligado em vez de falhar no meio do fluxo.
	ClientID string
	// Endpoints são sobrescritos apenas em teste.
	DeviceURL, TokenURL, IdentityURL string
	// MinInterval protege o servidor de um polling agressivo se ele devolver
	// um intervalo pequeno demais.
	MinInterval time.Duration
}

type Client struct{ config Config }

var _ usersync.Authenticator = (*Client)(nil)

// ErrNotConfigured explica a ausência do client_id sem expor o usuário a um
// erro cru do GitHub.
var ErrNotConfigured = errors.New(
	"este build não tem OAuth App do GitHub configurado; defina LEALING_GITHUB_CLIENT_ID")

func New(config Config) *Client {
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if config.DeviceURL == "" {
		config.DeviceURL = deviceURL
	}
	if config.TokenURL == "" {
		config.TokenURL = tokenURL
	}
	if config.IdentityURL == "" {
		config.IdentityURL = identityURL
	}
	if config.MinInterval <= 0 {
		config.MinInterval = time.Second
	}
	return &Client{config: config}
}

type deviceResponse struct {
	DeviceCode       string `json:"device_code"`
	UserCode         string `json:"user_code"`
	VerificationURI  string `json:"verification_uri"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *Client) RequestDevice(ctx context.Context) (usersync.DeviceCode, error) {
	if strings.TrimSpace(c.config.ClientID) == "" {
		return usersync.DeviceCode{}, ErrNotConfigured
	}
	var parsed deviceResponse
	if err := c.form(ctx, c.config.DeviceURL, url.Values{
		"client_id": {c.config.ClientID},
		"scope":     {scope},
	}, &parsed); err != nil {
		return usersync.DeviceCode{}, err
	}
	if parsed.Error != "" {
		return usersync.DeviceCode{}, describe(parsed.Error, parsed.ErrorDescription)
	}
	if parsed.DeviceCode == "" || parsed.UserCode == "" {
		return usersync.DeviceCode{}, errors.New("o GitHub não devolveu um código de dispositivo")
	}

	interval := time.Duration(parsed.Interval) * time.Second
	if interval < c.config.MinInterval {
		interval = c.config.MinInterval
	}
	expires := time.Now().Add(15 * time.Minute)
	if parsed.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	verification := parsed.VerificationURI
	if verification == "" {
		verification = "https://github.com/login/device"
	}
	return usersync.DeviceCode{
		Code: parsed.DeviceCode, UserCode: parsed.UserCode,
		VerificationURL: verification, Interval: interval, ExpiresAt: expires,
	}, nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Interval         int    `json:"interval"`
}

// Wait faz o polling até a aprovação. O intervalo obedece ao servidor: o
// GitHub responde slow_down quando julgamos rápido demais, e ignorar isso
// leva a bloqueio temporário do app inteiro — não só desta sessão.
func (c *Client) Wait(ctx context.Context, code usersync.DeviceCode) (usersync.Credential, error) {
	if strings.TrimSpace(c.config.ClientID) == "" {
		return usersync.Credential{}, ErrNotConfigured
	}
	interval := code.Interval
	if interval < c.config.MinInterval {
		interval = c.config.MinInterval
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return usersync.Credential{}, ctx.Err()
		case <-timer.C:
		}
		if !code.ExpiresAt.IsZero() && time.Now().After(code.ExpiresAt) {
			return usersync.Credential{}, errors.New("o código expirou; peça um novo")
		}

		var parsed tokenResponse
		if err := c.form(ctx, c.config.TokenURL, url.Values{
			"client_id":   {c.config.ClientID},
			"device_code": {code.Code},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &parsed); err != nil {
			return usersync.Credential{}, err
		}

		switch parsed.Error {
		case "":
			if parsed.AccessToken == "" {
				return usersync.Credential{}, errors.New("o GitHub aprovou mas não devolveu token")
			}
			return usersync.Credential{
				Token: parsed.AccessToken, Scope: parsed.Scope, ObtainedAt: time.Now().UTC(),
			}, nil
		case "authorization_pending":
			// Esperado: o usuário ainda não terminou no browser.
		case "slow_down":
			interval += 5 * time.Second
			if parsed.Interval > 0 {
				interval = time.Duration(parsed.Interval) * time.Second
			}
		default:
			return usersync.Credential{}, describe(parsed.Error, parsed.ErrorDescription)
		}
		timer.Reset(interval)
	}
}

func (c *Client) Identity(ctx context.Context, credential usersync.Credential) (usersync.Identity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.IdentityURL, nil)
	if err != nil {
		return usersync.Identity{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := c.config.Client.Do(request)
	if err != nil {
		return usersync.Identity{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return usersync.Identity{}, err
	}
	if response.StatusCode != http.StatusOK {
		return usersync.Identity{}, fmt.Errorf("GitHub respondeu %s ao identificar a conta", response.Status)
	}
	var parsed struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return usersync.Identity{}, err
	}
	if parsed.Login == "" {
		return usersync.Identity{}, errors.New("o GitHub não devolveu o login da conta")
	}
	return usersync.Identity{Login: parsed.Login, Name: parsed.Name}, nil
}

// form faz o POST de formulário pedindo JSON de volta. Sem o Accept, o
// GitHub responde estes endpoints em urlencoded.
func (c *Client) form(ctx context.Context, endpoint string, values url.Values, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.config.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return err
	}
	if response.StatusCode >= 500 {
		return fmt.Errorf("GitHub respondeu %s", response.Status)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("resposta inesperada do GitHub (%s)", response.Status)
	}
	return nil
}

// describe traduz os erros do device flow para o que o usuário precisa
// fazer. A descrição crua do GitHub vem em inglês e fala de "device code",
// que não é vocabulário de quem só queria entrar na conta.
func describe(code, description string) error {
	switch code {
	case "access_denied":
		return errors.New("autorização negada no GitHub")
	case "expired_token":
		return errors.New("o código expirou; peça um novo")
	case "incorrect_device_code":
		return errors.New("o código não confere; peça um novo")
	case "device_flow_disabled":
		return errors.New("o OAuth App deste build está com o device flow desligado")
	case "unauthorized_client":
		return errors.New("o OAuth App deste build não está autorizado a usar device flow")
	}
	if description != "" {
		return fmt.Errorf("GitHub: %s (%s)", description, code)
	}
	return fmt.Errorf("GitHub recusou a autenticação (%s)", code)
}
