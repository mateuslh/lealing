// Package toolstate persiste quais tools o usuário desativou.
package toolstate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

const (
	currentVersion = 3
	sizeLimit      = 1 << 20
	maxEntries     = 4096
	maxIDBytes     = 256
	maxHostBytes   = 512
	lockRetry      = 10 * time.Millisecond
	staleLockAge   = time.Minute
)

var ErrConflict = errors.New("estado de tools mudou em outro processo")

type fileToolRef struct {
	ID   string `json:"id"`
	Host string `json:"host"`
}

type fileStateV3 struct {
	Version    int           `json:"version"`
	Generation uint64        `json:"generation"`
	Disabled   []fileToolRef `json:"disabled,omitempty"`
	Checksum   string        `json:"checksum"`
}

type checksumPayload struct {
	Version    int           `json:"version"`
	Generation uint64        `json:"generation"`
	Disabled   []fileToolRef `json:"disabled,omitempty"`
}

type decodedFile struct {
	state      toolmanage.State
	generation uint64
	raw        []byte
	etag       [sha256.Size]byte
	exists     bool
}

type Store struct {
	path string
	now  func() time.Time

	mutex      sync.Mutex
	loaded     bool
	generation uint64
	etag       [sha256.Size]byte
}

var _ toolmanage.Store = (*Store)(nil)

func New(path string) *Store { return &Store{path: path, now: time.Now} }

func (s *Store) Load(ctx context.Context) (toolmanage.State, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	decoded, err := s.loadBest(ctx)
	if err != nil {
		return toolmanage.State{}, err
	}
	s.remember(decoded)
	return cloneState(decoded.state), nil
}

func (s *Store) Save(ctx context.Context, state toolmanage.State) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	disabled, err := normalizeRefs(state.Disabled)
	if err != nil {
		return err
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	release, err := s.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer release()

	current, err := s.loadBest(ctx)
	if err != nil {
		return err
	}
	if s.loaded && (current.etag != s.etag || current.generation != s.generation) {
		return ErrConflict
	}
	if current.generation == ^uint64(0) {
		return errors.New("geração do estado de tools esgotada")
	}

	document := fileStateV3{
		Version: currentVersion, Generation: current.generation + 1, Disabled: disabled,
	}
	document.Checksum, err = checksum(document.Generation, document.Disabled)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > sizeLimit {
		return fmt.Errorf("estado de tools excede %d bytes", sizeLimit)
	}

	// O backup é a última versão confirmada, nunca a tentativa nova. Assim,
	// uma falha no rename principal não faz uma escrita reportada como falha
	// reaparecer no próximo arranque.
	if current.exists {
		if err := atomicWrite(ctx, s.backupPath(), current.raw); err != nil {
			return fmt.Errorf("guardar backup do estado de tools: %w", err)
		}
	}
	if err := atomicWrite(ctx, s.path, body); err != nil {
		return fmt.Errorf("gravar estado de tools: %w", err)
	}
	// Depois do commit principal, atualiza a redundância. Esta etapa é best
	// effort: devolver erro aqui faria o core manter o estado antigo em
	// memória embora o novo já estivesse confirmado no disco.
	_ = atomicWrite(ctx, s.backupPath(), body)

	s.loaded = true
	s.generation = document.Generation
	s.etag = sha256.Sum256(body)
	return nil
}

func (s *Store) loadBest(ctx context.Context) (decodedFile, error) {
	primary, primaryErr := readFile(ctx, s.path)
	if primaryErr == nil && primary.exists {
		return primary, nil
	}
	backup, backupErr := readFile(ctx, s.backupPath())
	if backupErr == nil && backup.exists {
		return backup, nil
	}
	if primaryErr == nil && backupErr == nil {
		return decodedFile{}, nil
	}
	if primaryErr != nil && backupErr != nil && !errors.Is(backupErr, fs.ErrNotExist) {
		return decodedFile{}, fmt.Errorf("estado principal inválido (%v); backup inválido (%v)", primaryErr, backupErr)
	}
	if primaryErr != nil {
		return decodedFile{}, primaryErr
	}
	return decodedFile{}, backupErr
}

func (s *Store) remember(decoded decodedFile) {
	s.loaded = true
	s.generation = decoded.generation
	s.etag = decoded.etag
}

func (s *Store) backupPath() string { return s.path + ".bak" }
func (s *Store) lockPath() string   { return s.path + ".lock" }

func (s *Store) acquireLock(ctx context.Context) (func(), error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	path := s.lockPath()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if _, err := file.WriteString(token + "\n"); err != nil {
				file.Close()
				_ = os.Remove(path)
				return nil, err
			}
			if err := file.Sync(); err != nil {
				file.Close()
				_ = os.Remove(path)
				return nil, err
			}
			if err := file.Close(); err != nil {
				_ = os.Remove(path)
				return nil, err
			}
			return func() { releaseLock(path, token) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		stale, staleErr := s.staleLock(path)
		if staleErr != nil {
			return nil, staleErr
		}
		if stale {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			continue
		}
		timer := time.NewTimer(lockRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) staleLock(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("lock de estado inseguro em %s", path)
	}
	return s.now().Sub(info.ModTime()) > staleLockAge, nil
}

func releaseLock(path, token string) {
	raw, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(raw)) == token {
		_ = os.Remove(path)
	}
}

func readFile(ctx context.Context, path string) (decodedFile, error) {
	if err := ctx.Err(); err != nil {
		return decodedFile{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return decodedFile{}, nil
	}
	if err != nil {
		return decodedFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return decodedFile{}, fmt.Errorf("estado de tools em %s não é arquivo regular", path)
	}
	if info.Size() > sizeLimit {
		return decodedFile{}, fmt.Errorf("estado de tools em %s excede %d bytes", path, sizeLimit)
	}
	file, err := os.Open(path)
	if err != nil {
		return decodedFile{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, sizeLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return decodedFile{}, readErr
	}
	if closeErr != nil {
		return decodedFile{}, closeErr
	}
	if len(raw) > sizeLimit {
		return decodedFile{}, fmt.Errorf("estado de tools em %s excede %d bytes", path, sizeLimit)
	}
	if err := ctx.Err(); err != nil {
		return decodedFile{}, err
	}
	decoded, err := decode(raw)
	if err != nil {
		return decodedFile{}, fmt.Errorf("estado de tools em %s: %w", path, err)
	}
	decoded.raw = append([]byte(nil), raw...)
	decoded.etag = sha256.Sum256(raw)
	decoded.exists = true
	return decoded, nil
}

func decode(raw []byte) (decodedFile, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return decodedFile{}, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return decodedFile{}, err
	}

	if header.Version == currentVersion {
		var document fileStateV3
		if err := decodeStrict(raw, &document); err != nil {
			return decodedFile{}, err
		}
		refs, err := normalizeRefsFromFile(document.Disabled)
		if err != nil {
			return decodedFile{}, err
		}
		if !sameFileRefs(document.Disabled, refs) {
			return decodedFile{}, errors.New("estado de tools não está em ordem canônica")
		}
		want, err := checksum(document.Generation, document.Disabled)
		if err != nil {
			return decodedFile{}, err
		}
		if document.Checksum != want {
			return decodedFile{}, errors.New("checksum do estado de tools não confere")
		}
		return decodedFile{
			state: toolmanage.State{Disabled: refs}, generation: document.Generation,
		}, nil
	}
	return decodedFile{}, fmt.Errorf("versão de estado de tools não suportada: %d", header.Version)
}

func sameFileRefs(values []fileToolRef, refs []domain.ToolRef) bool {
	if len(values) != len(refs) {
		return false
	}
	for index := range values {
		if values[index].ID != string(refs[index].ID) || values[index].Host != refs[index].Host {
			return false
		}
	}
	return true
}

func normalizeRefs(refs []domain.ToolRef) ([]fileToolRef, error) {
	if len(refs) > maxEntries {
		return nil, fmt.Errorf("estado de tools excede %d referências", maxEntries)
	}
	disabled := make([]fileToolRef, 0, len(refs))
	seen := make(map[domain.ToolRef]bool, len(refs))
	for _, ref := range refs {
		if err := validateRef(ref); err != nil {
			return nil, err
		}
		if seen[ref] {
			return nil, errors.New("estado de tools contém identidade duplicada")
		}
		seen[ref] = true
		disabled = append(disabled, fileToolRef{ID: string(ref.ID), Host: ref.Host})
	}
	sort.Slice(disabled, func(i, j int) bool {
		if disabled[i].Host != disabled[j].Host {
			return disabled[i].Host < disabled[j].Host
		}
		return disabled[i].ID < disabled[j].ID
	})
	return disabled, nil
}

func normalizeRefsFromFile(values []fileToolRef) ([]domain.ToolRef, error) {
	refs := make([]domain.ToolRef, 0, len(values))
	for _, value := range values {
		refs = append(refs, domain.ToolRef{ID: domain.ToolID(value.ID), Host: value.Host})
	}
	normalized, err := normalizeRefs(refs)
	if err != nil {
		return nil, err
	}
	result := make([]domain.ToolRef, 0, len(normalized))
	for _, value := range normalized {
		result = append(result, domain.ToolRef{ID: domain.ToolID(value.ID), Host: value.Host})
	}
	return result, nil
}

func validateRef(ref domain.ToolRef) error {
	id, host := string(ref.ID), ref.Host
	switch {
	case id == "" || host == "":
		return errors.New("estado de tools contém identidade vazia")
	case id != strings.TrimSpace(id) || host != strings.TrimSpace(host):
		return errors.New("estado de tools contém identidade não canônica")
	case len(id) > maxIDBytes || len(host) > maxHostBytes:
		return errors.New("estado de tools contém identidade longa demais")
	case !utf8.ValidString(id) || !utf8.ValidString(host):
		return errors.New("estado de tools contém UTF-8 inválido")
	case hasControl(id) || hasControl(host):
		return errors.New("estado de tools contém caracteres de controle")
	default:
		return nil
	}
}

func hasControl(value string) bool {
	for _, symbol := range value {
		if unicode.IsControl(symbol) {
			return true
		}
	}
	return false
}

func checksum(generation uint64, disabled []fileToolRef) (string, error) {
	payload, err := json.Marshal(checksumPayload{
		Version: currentVersion, Generation: generation, Disabled: disabled,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkJSON(decoder); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func walkJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("objeto JSON contém chave inválida")
			}
			if seen[key] {
				return fmt.Errorf("objeto JSON contém chave duplicada %q", key)
			}
			seen[key] = true
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("objeto JSON não foi encerrado")
		}
	case '[':
		for decoder.More() {
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("array JSON não foi encerrado")
		}
	default:
		return errors.New("delimitador JSON inesperado")
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contém dados adicionais")
		}
		return err
	}
	return nil
}

func atomicWrite(ctx context.Context, path string, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".tool-state-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err := ctx.Err(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	// O rename já confirmou a troca lógica. Sincronizar o diretório reduz a
	// janela de perda após queda de energia; alguns filesystems não oferecem
	// fsync de diretório, então essa garantia adicional é best effort.
	if directoryFile, err := os.Open(directory); err == nil {
		_ = directoryFile.Sync()
		_ = directoryFile.Close()
	}
	return nil
}

func cloneState(state toolmanage.State) toolmanage.State {
	return toolmanage.State{Disabled: append([]domain.ToolRef(nil), state.Disabled...)}
}
