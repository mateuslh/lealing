// Package devkit implementa as ferramentas de engenharia com biblioteca
// padrão e integrações de rede limitadas.
package devkit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	core "github.com/mateuslh/lealing/internal/core/devkit"
)

const (
	maxHTTPBody  = 64 << 10
	maxRedirects = 5
)

// Runner implementa a porta central da bancada.
type Runner struct {
	client   *http.Client
	resolver *net.Resolver
	now      func() time.Time
	random   io.Reader
}

var _ core.Runner = (*Runner)(nil)

// New monta o runner de produção.
func New() *Runner {
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("limite de 5 redirecionamentos excedido")
			}
			return nil
		},
	}
	return &Runner{
		client:   client,
		resolver: net.DefaultResolver,
		now:      time.Now,
		random:   rand.Reader,
	}
}

// Run despacha uma operação validada.
func (r *Runner) Run(ctx context.Context, request core.Request) (core.Result, error) {
	if err := core.ValidateRequest(request); err != nil {
		return core.Result{}, err
	}

	switch request.Tool {
	case core.ToolHTTP:
		return r.probeHTTP(ctx, request.Mode, request.Input)
	case core.ToolNetwork:
		if request.Mode == "dns" {
			return r.inspectDNS(ctx, request.Input)
		}
		return r.inspectTLS(ctx, request.Input)
	case core.ToolJSON:
		return FormatJSON(request.Mode, request.Input)
	case core.ToolJWT:
		return InspectJWT(request.Mode, request.Input, r.now())
	case core.ToolCIDR:
		return CalculateCIDR(request.Input)
	case core.ToolCodec:
		return TransformCodec(request.Mode, request.Input)
	case core.ToolChecksum:
		return Checksum(request.Mode, request.Input)
	case core.ToolUUID:
		return GenerateUUIDs(request.Mode, request.Input, r.now(), r.random)
	default:
		return core.Result{}, fmt.Errorf("tool sem implementação: %s", request.Tool)
	}
}

func (r *Runner) probeHTTP(ctx context.Context, method, rawURL string) (core.Result, error) {
	target := strings.TrimSpace(rawURL)
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.Host == "" {
		return core.Result{}, errors.New("informe uma URL HTTP ou HTTPS completa")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return core.Result{}, errors.New("somente os esquemas http e https são aceitos")
	}
	if parsed.User != nil {
		return core.Result{}, errors.New("credenciais embutidas na URL não são aceitas")
	}

	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), nil)
	if err != nil {
		return core.Result{}, err
	}
	req.Header.Set("User-Agent", "lealing-http-probe/1")
	req.Header.Set("Accept", "*/*")

	started := time.Now()
	resp, err := r.client.Do(req)
	if err != nil {
		return core.Result{}, fmt.Errorf("requisição %s: %w", method, err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(started).Round(time.Millisecond)

	contentType := resp.Header.Get("Content-Type")
	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		contentLength = "não informado"
	}
	finalURL := *resp.Request.URL
	finalURL.User = nil

	result := core.Result{
		Title:   method + " concluído",
		Summary: fmt.Sprintf("%s em %s", resp.Status, elapsed),
		Rows: []core.Row{
			{Label: "Status", Value: resp.Status},
			{Label: "Protocolo", Value: resp.Proto},
			{Label: "Latência", Value: elapsed.String()},
			{Label: "URL final", Value: safeText(finalURL.String())},
			{Label: "Tipo", Value: fallback(contentType, "não informado")},
			{Label: "Tamanho", Value: contentLength},
		},
	}
	if server := resp.Header.Get("Server"); server != "" {
		result.Rows = append(result.Rows, core.Row{Label: "Server", Value: safeText(server)})
	}
	if requestID := firstHeader(resp.Header, "X-Request-Id", "X-Correlation-Id", "Traceparent"); requestID != "" {
		result.Rows = append(result.Rows, core.Row{Label: "Request ID", Value: safeText(requestID)})
	}

	if method == http.MethodHead {
		result.Body = "HEAD não baixa o corpo da resposta."
		return result, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+1))
	if err != nil {
		return core.Result{}, fmt.Errorf("ler corpo: %w", err)
	}
	truncated := len(body) > maxHTTPBody
	if truncated {
		body = body[:maxHTTPBody]
		result.Warning = "Prévia limitada a 64 KiB."
	}
	if len(body) == 0 {
		result.Body = "Resposta sem corpo."
	} else if isTextual(contentType, body) {
		result.Body = safeText(string(body))
	} else {
		result.Body = fmt.Sprintf("Corpo binário omitido (%d bytes lidos).", len(body))
	}
	return result, nil
}

func (r *Runner) inspectDNS(ctx context.Context, rawHost string) (core.Result, error) {
	host, _, err := normalizedHost(rawHost, "")
	if err != nil {
		return core.Result{}, err
	}
	addresses, err := r.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return core.Result{}, fmt.Errorf("resolver %s: %w", host, err)
	}
	cname, _ := r.resolver.LookupCNAME(ctx, host)

	rows := []core.Row{{Label: "Host", Value: host}}
	if cname != "" && strings.TrimSuffix(cname, ".") != strings.TrimSuffix(host, ".") {
		rows = append(rows, core.Row{Label: "CNAME", Value: strings.TrimSuffix(cname, ".")})
	}
	var values []string
	for _, address := range addresses {
		values = append(values, address.IP.String())
	}
	if len(values) == 0 {
		return core.Result{}, errors.New("a consulta não devolveu endereços")
	}
	rows = append(rows, core.Row{Label: "Endereços", Value: strconv.Itoa(len(values))})
	return core.Result{
		Title:   "DNS resolvido",
		Summary: fmt.Sprintf("%d endereço(s) para %s", len(values), host),
		Rows:    rows,
		Body:    strings.Join(values, "\n"),
	}, nil
}

func (r *Runner) inspectTLS(ctx context.Context, rawHost string) (core.Result, error) {
	host, port, err := normalizedHost(rawHost, "443")
	if err != nil {
		return core.Result{}, err
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		// A inspeção precisa mostrar também certificados vencidos ou de
		// laboratório. A verificação é refeita abaixo e o erro vira aviso.
		Config: &tls.Config{ServerName: host, InsecureSkipVerify: true}, //nolint:gosec
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return core.Result{}, fmt.Errorf("handshake TLS: %w", err)
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return core.Result{}, errors.New("a conexão não expôs estado TLS")
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return core.Result{}, errors.New("o servidor não apresentou certificado")
	}
	leaf := state.PeerCertificates[0]

	now := r.now()
	intermediates := x509.NewCertPool()
	for _, certificate := range state.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	verification := "cadeia confiável"
	warning := ""
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       host,
		Intermediates: intermediates,
		CurrentTime:   now,
	}); err != nil {
		verification = "cadeia não confiável"
		warning = err.Error()
	}

	names := append([]string(nil), leaf.DNSNames...)
	if len(names) == 0 && leaf.Subject.CommonName != "" {
		names = append(names, leaf.Subject.CommonName)
	}
	body := "SANs:\n" + strings.Join(names, "\n")
	if len(names) == 0 {
		body = "O certificado não declara nomes DNS."
	}

	return core.Result{
		Title:   "TLS inspecionado",
		Summary: fmt.Sprintf("%s · %s", tlsVersion(state.Version), verification),
		Rows: []core.Row{
			{Label: "Destino", Value: net.JoinHostPort(host, port)},
			{Label: "Versão", Value: tlsVersion(state.Version)},
			{Label: "Cifra", Value: tls.CipherSuiteName(state.CipherSuite)},
			{Label: "Emissor", Value: fallback(leaf.Issuer.CommonName, leaf.Issuer.String())},
			{Label: "Início", Value: leaf.NotBefore.Local().Format(time.RFC3339)},
			{Label: "Expira", Value: leaf.NotAfter.Local().Format(time.RFC3339)},
			{Label: "Restante", Value: validityDistance(now, leaf.NotAfter)},
			{Label: "Cadeia", Value: fmt.Sprintf("%d certificado(s)", len(state.PeerCertificates))},
		},
		Body:    safeText(body),
		Warning: safeText(warning),
	}, nil
}

// FormatJSON valida e transforma um documento JSON completo.
func FormatJSON(mode, input string) (core.Result, error) {
	raw := []byte(strings.TrimSpace(input))
	if len(raw) == 0 {
		return core.Result{}, errors.New("cole um documento JSON")
	}
	if !json.Valid(raw) {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return core.Result{}, fmt.Errorf("JSON inválido: %w", err)
		}
		return core.Result{}, errors.New("JSON inválido")
	}

	var out bytes.Buffer
	var err error
	switch mode {
	case "format":
		err = json.Indent(&out, raw, "", "  ")
	case "minify":
		err = json.Compact(&out, raw)
	default:
		return core.Result{}, fmt.Errorf("modo JSON desconhecido: %s", mode)
	}
	if err != nil {
		return core.Result{}, err
	}
	return core.Result{
		Title:   "JSON válido",
		Summary: fmt.Sprintf("%d → %d bytes", len(raw), out.Len()),
		Rows: []core.Row{
			{Label: "Entrada", Value: fmt.Sprintf("%d bytes", len(raw))},
			{Label: "Saída", Value: fmt.Sprintf("%d bytes", out.Len())},
		},
		Body: out.String(),
	}, nil
}

// InspectJWT decodifica as partes públicas de um JWT sem afirmar que a
// assinatura é válida.
func InspectJWT(mode, input string, now time.Time) (core.Result, error) {
	parts := strings.Split(strings.TrimSpace(input), ".")
	if len(parts) != 3 {
		return core.Result{}, errors.New("um JWT precisa ter header, payload e assinatura")
	}
	header, err := decodeJWTPart(parts[0])
	if err != nil {
		return core.Result{}, fmt.Errorf("header JWT: %w", err)
	}
	payload, err := decodeJWTPart(parts[1])
	if err != nil {
		return core.Result{}, fmt.Errorf("payload JWT: %w", err)
	}

	var headerClaims, claims map[string]any
	if err := decodeJSONObject(header, &headerClaims); err != nil {
		return core.Result{}, fmt.Errorf("header JWT: %w", err)
	}
	if err := decodeJSONObject(payload, &claims); err != nil {
		return core.Result{}, fmt.Errorf("payload JWT: %w", err)
	}

	rows := []core.Row{
		{Label: "Algoritmo", Value: claimString(headerClaims["alg"])},
		{Label: "Tipo", Value: fallback(claimString(headerClaims["typ"]), "não declarado")},
		{Label: "Issuer", Value: fallback(claimString(claims["iss"]), "não declarado")},
		{Label: "Subject", Value: fallback(claimString(claims["sub"]), "não declarado")},
		{Label: "Audience", Value: fallback(claimString(claims["aud"]), "não declarada")},
	}
	for _, temporal := range []struct {
		key, label string
	}{
		{"iat", "Emitido"}, {"nbf", "Válido após"}, {"exp", "Expira"},
	} {
		if value, ok := numericDate(claims[temporal.key]); ok {
			rows = append(rows, core.Row{Label: temporal.label, Value: time.Unix(value, 0).Local().Format(time.RFC3339)})
		}
	}

	var body string
	if mode == "raw" {
		body = "HEADER\n" + string(header) + "\n\nPAYLOAD\n" + string(payload)
	} else {
		var prettyHeader, prettyPayload bytes.Buffer
		_ = json.Indent(&prettyHeader, header, "", "  ")
		_ = json.Indent(&prettyPayload, payload, "", "  ")
		body = "HEADER\n" + prettyHeader.String() + "\n\nCLAIMS\n" + prettyPayload.String()
	}

	summary := "claims decodificadas"
	if exp, ok := numericDate(claims["exp"]); ok {
		delta := time.Unix(exp, 0).Sub(now)
		if delta <= 0 {
			summary = "token expirado há " + durationLabel(-delta)
		} else {
			summary = "expira em " + durationLabel(delta)
		}
	}
	return core.Result{
		Title:   "JWT decodificado",
		Summary: summary,
		Rows:    rows,
		Body:    safeText(body),
		Warning: "A assinatura não foi verificada; conteúdo decodificado não prova autenticidade.",
	}, nil
}

// CalculateCIDR devolve limites e capacidade de um prefixo IP.
func CalculateCIDR(input string) (core.Result, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(input))
	if err != nil {
		return core.Result{}, fmt.Errorf("CIDR inválido: %w", err)
	}
	prefix = prefix.Masked()
	first := prefix.Addr()
	last := lastAddress(prefix)
	bits := 128
	if prefix.Addr().Is4() {
		bits = 32
	}
	hostBits := bits - prefix.Bits()
	total := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))

	rows := []core.Row{
		{Label: "Rede", Value: prefix.String()},
		{Label: "Primeiro", Value: first.String()},
		{Label: "Último", Value: last.String()},
		{Label: "Endereços", Value: total.String()},
		{Label: "Bits de host", Value: strconv.Itoa(hostBits)},
	}
	if prefix.Addr().Is4() {
		mask := net.CIDRMask(prefix.Bits(), 32)
		rows = append(rows,
			core.Row{Label: "Máscara", Value: net.IP(mask).String()},
			core.Row{Label: "Broadcast", Value: last.String()},
		)
		if hostBits >= 2 {
			usable := new(big.Int).Sub(total, big.NewInt(2))
			rows = append(rows,
				core.Row{Label: "Hosts úteis", Value: usable.String()},
				core.Row{Label: "Faixa útil", Value: first.Next().String() + " — " + previous(last).String()},
			)
		}
	}
	return core.Result{
		Title:   "Bloco calculado",
		Summary: prefix.String() + " contém " + total.String() + " endereço(s)",
		Rows:    rows,
		Body:    "Cálculo local; nenhuma consulta de rede foi realizada.",
	}, nil
}

// TransformCodec executa codecs textuais reversíveis.
func TransformCodec(mode, input string) (core.Result, error) {
	var output, label string
	var err error
	switch mode {
	case "b64-encode":
		label = "Base64"
		output = base64.StdEncoding.EncodeToString([]byte(input))
	case "b64-decode":
		label = "UTF-8"
		var decoded []byte
		decoded, err = base64.StdEncoding.DecodeString(strings.TrimSpace(input))
		if err == nil {
			if !utf8.Valid(decoded) {
				return core.Result{}, errors.New("o resultado Base64 não é texto UTF-8")
			}
			output = string(decoded)
		}
	case "b64url-encode":
		label = "Base64URL"
		output = base64.RawURLEncoding.EncodeToString([]byte(input))
	case "b64url-decode":
		label = "UTF-8"
		var decoded []byte
		decoded, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(input), "="))
		if err == nil {
			if !utf8.Valid(decoded) {
				return core.Result{}, errors.New("o resultado Base64URL não é texto UTF-8")
			}
			output = string(decoded)
		}
	case "url-encode":
		label = "URL encoded"
		output = url.QueryEscape(input)
	case "url-decode":
		label = "UTF-8"
		output, err = url.QueryUnescape(input)
	default:
		return core.Result{}, fmt.Errorf("codec desconhecido: %s", mode)
	}
	if err != nil {
		return core.Result{}, fmt.Errorf("transformar: %w", err)
	}
	return core.Result{
		Title:   "Transformação concluída",
		Summary: fmt.Sprintf("%d → %d bytes", len(input), len(output)),
		Rows:    []core.Row{{Label: "Formato", Value: label}},
		Body:    safeText(output),
	}, nil
}

// Checksum calcula um digest hexadecimal.
func Checksum(mode, input string) (core.Result, error) {
	var algorithm, digest, warning string
	switch mode {
	case "sha256":
		sum := sha256.Sum256([]byte(input))
		algorithm, digest = "SHA-256", hex.EncodeToString(sum[:])
	case "sha512":
		sum := sha512.Sum512([]byte(input))
		algorithm, digest = "SHA-512", hex.EncodeToString(sum[:])
	case "sha1":
		sum := sha1.Sum([]byte(input)) //nolint:gosec
		algorithm, digest = "SHA-1", hex.EncodeToString(sum[:])
		warning = "SHA-1 é legado; não use para assinatura, senha ou decisão de segurança."
	default:
		return core.Result{}, fmt.Errorf("algoritmo desconhecido: %s", mode)
	}
	return core.Result{
		Title:   "Checksum gerado",
		Summary: algorithm + " · " + strconv.Itoa(len(input)) + " bytes",
		Rows:    []core.Row{{Label: "Algoritmo", Value: algorithm}, {Label: "Bytes", Value: strconv.Itoa(len(input))}},
		Body:    digest,
		Warning: warning,
	}, nil
}

// GenerateUUIDs gera de 1 a 100 identificadores.
func GenerateUUIDs(mode, rawCount string, now time.Time, random io.Reader) (core.Result, error) {
	count := 1
	if strings.TrimSpace(rawCount) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(rawCount))
		if err != nil {
			return core.Result{}, errors.New("quantidade precisa ser um número de 1 a 100")
		}
		count = parsed
	}
	if count < 1 || count > 100 {
		return core.Result{}, errors.New("quantidade precisa ficar entre 1 e 100")
	}

	values := make([]string, count)
	for i := range count {
		value, err := generateUUID(mode, now, random)
		if err != nil {
			return core.Result{}, fmt.Errorf("gerar UUID: %w", err)
		}
		values[i] = value
		// UUID v7 do mesmo lote compartilha o milissegundo, mas os 74 bits
		// aleatórios restantes mantêm cada identificador independente.
	}
	version := strings.ToUpper(mode)
	return core.Result{
		Title:   "UUIDs gerados",
		Summary: fmt.Sprintf("%d UUID %s", count, version),
		Rows:    []core.Row{{Label: "Versão", Value: version}, {Label: "Quantidade", Value: strconv.Itoa(count)}},
		Body:    strings.Join(values, "\n"),
	}, nil
}

func generateUUID(mode string, now time.Time, random io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", err
	}
	switch mode {
	case "v4":
		value[6] = (value[6] & 0x0f) | 0x40
	case "v7":
		millis := uint64(now.UnixMilli())
		value[0] = byte(millis >> 40)
		value[1] = byte(millis >> 32)
		value[2] = byte(millis >> 24)
		value[3] = byte(millis >> 16)
		value[4] = byte(millis >> 8)
		value[5] = byte(millis)
		value[6] = (value[6] & 0x0f) | 0x70
	default:
		return "", fmt.Errorf("versão desconhecida: %s", mode)
	}
	value[8] = (value[8] & 0x3f) | 0x80
	return formatUUID(value), nil
}

func formatUUID(value [16]byte) string {
	var text [36]byte
	hex.Encode(text[0:8], value[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], value[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], value[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], value[8:10])
	text[23] = '-'
	hex.Encode(text[24:36], value[10:16])
	return string(text[:])
}

func decodeJWTPart(part string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(part, "="))
	if err != nil {
		return nil, err
	}
	if !json.Valid(decoded) {
		return nil, errors.New("conteúdo não é JSON")
	}
	return decoded, nil
}

func decodeJSONObject(raw []byte, target *map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func claimString(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		values := make([]string, 0, len(value))
		for _, item := range value {
			values = append(values, fmt.Sprint(item))
		}
		return strings.Join(values, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func numericDate(value any) (int64, bool) {
	switch value := value.(type) {
	case json.Number:
		integer, err := value.Int64()
		return integer, err == nil
	case float64:
		return int64(value), true
	case int64:
		return value, true
	default:
		return 0, false
	}
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Masked().Addr().As16()
	offset := 0
	if prefix.Addr().Is4() {
		offset = 12
	}
	bits := prefix.Bits()
	if prefix.Addr().Is4() {
		bits += 96
	}
	for bit := bits; bit < 128; bit++ {
		index := bit / 8
		bytes[index] |= 1 << (7 - uint(bit%8))
	}
	addr := netip.AddrFrom16(bytes)
	if offset == 12 {
		return addr.Unmap()
	}
	return addr
}

func previous(address netip.Addr) netip.Addr {
	bytes := address.As16()
	for i := len(bytes) - 1; i >= 0; i-- {
		if bytes[i] > 0 {
			bytes[i]--
			break
		}
		bytes[i] = 0xff
	}
	out := netip.AddrFrom16(bytes)
	if address.Is4() {
		return out.Unmap()
	}
	return out
}

func normalizedHost(input, defaultPort string) (string, string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", errors.New("informe um host")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" {
			return "", "", errors.New("host inválido")
		}
		value = parsed.Host
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		if strings.Contains(value, ":") && net.ParseIP(value) == nil {
			return "", "", errors.New("use host:porta ou somente o host")
		}
		host, port = strings.Trim(value, "[]"), defaultPort
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || strings.ContainsAny(host, "/?#") {
		return "", "", errors.New("host inválido")
	}
	return host, port, nil
}

func tlsVersion(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("TLS 0x%04x", version)
	}
}

func validityDistance(now, expiry time.Time) string {
	delta := expiry.Sub(now)
	if delta < 0 {
		return "expirado há " + durationLabel(-delta)
	}
	return durationLabel(delta)
}

func durationLabel(duration time.Duration) string {
	if days := int(duration.Hours() / 24); days > 0 {
		return fmt.Sprintf("%d dia(s)", days)
	}
	if hours := int(duration.Hours()); hours > 0 {
		return fmt.Sprintf("%d hora(s)", hours)
	}
	return fmt.Sprintf("%d minuto(s)", max(int(duration.Minutes()), 0))
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func isTextual(contentType string, body []byte) bool {
	contentType = strings.ToLower(contentType)
	if strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "yaml") {
		return true
	}
	return utf8.Valid(body)
}

// safeText remove controles capazes de mover o cursor ou injetar estilo no
// terminal. Tabs viram espaços para não terem largura diferente por terminal.
func safeText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func fallback(value, alternative string) string {
	if value == "" {
		return alternative
	}
	return value
}
