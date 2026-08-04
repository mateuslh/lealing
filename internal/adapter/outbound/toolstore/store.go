// Package toolstore instala tools em diretórios versionados sem executá-las.
package toolstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

type Store struct {
	root       string
	categories map[domain.CategoryID]bool
	target     toolmanifest.Target
	now        func() time.Time
	mutex      sync.Mutex
}

var validID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var _ toolinstall.Store = (*Store)(nil)

func New(root string, categories []domain.Category, target toolmanifest.Target, now func() time.Time) *Store {
	known := make(map[domain.CategoryID]bool, len(categories))
	for _, category := range categories {
		known[category.ID] = true
	}
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, categories: known, target: target, now: now}
}

func (s *Store) Install(ctx context.Context, request toolinstall.InstallRequest) (_ toolinstall.Installation, resultErr error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.install(ctx, request)
}

func (s *Store) install(ctx context.Context, request toolinstall.InstallRequest) (_ toolinstall.Installation, resultErr error) {
	if err := ctx.Err(); err != nil {
		return toolinstall.Installation{}, err
	}
	if request.Host == "" {
		request.Host = "local"
	}
	if err := safeID(request.Host); err != nil {
		return toolinstall.Installation{}, fmt.Errorf("host da tool: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(request.SourceDir, "manifest.yaml"))
	if err != nil {
		return toolinstall.Installation{}, fmt.Errorf("ler manifest local: %w", err)
	}
	manifest, err := toolmanifest.ParseAndValidate(raw, toolmanifest.ValidationOptions{Categories: s.categories, Target: s.target})
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if request.ExpectedID != "" && manifest.ID != request.ExpectedID {
		return toolinstall.Installation{}, fmt.Errorf("manifest declara ID %s, índice selecionou %s", manifest.ID, request.ExpectedID)
	}
	if request.ExpectedVersion != "" && manifest.Version != request.ExpectedVersion {
		return toolinstall.Installation{}, fmt.Errorf("manifest declara versão %s, índice selecionou %s", manifest.Version, request.ExpectedVersion)
	}
	if request.ExpectedManifest != nil {
		if err := matchesExpectation(manifest, *request.ExpectedManifest); err != nil {
			return toolinstall.Installation{}, err
		}
	}
	if !manifest.Supports(s.target) {
		return toolinstall.Installation{}, fmt.Errorf("tool %s não publica artefato para %s-%s", manifest.ID, s.target.OS, s.target.Arch)
	}

	executableName := manifest.ExecutableName(s.target)
	sourceExecutable := filepath.Join(request.SourceDir, executableName)
	checksum, mode, err := inspectArtifact(sourceExecutable)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if request.ExpectedSHA256 != "" && !strings.EqualFold(checksum, request.ExpectedSHA256) {
		return toolinstall.Installation{}, fmt.Errorf("checksum não confere: esperado %s, recebido %s", request.ExpectedSHA256, checksum)
	}

	idDir := filepath.Join(s.root, manifest.ID)
	versionDir := filepath.Join(idDir, manifest.Version)
	if _, err := os.Stat(versionDir); err == nil {
		return toolinstall.Installation{}, fmt.Errorf("%s@%s já está instalada", manifest.ID, manifest.Version)
	} else if !os.IsNotExist(err) {
		return toolinstall.Installation{}, err
	}
	if err := os.MkdirAll(idDir, 0o755); err != nil {
		return toolinstall.Installation{}, err
	}
	temporary, err := os.MkdirTemp(idDir, ".install-")
	if err != nil {
		return toolinstall.Installation{}, err
	}
	defer func() {
		if resultErr != nil {
			_ = os.RemoveAll(temporary)
		}
	}()

	if err := copyFile(filepath.Join(temporary, "manifest.yaml"), filepath.Join(request.SourceDir, "manifest.yaml"), 0o600); err != nil {
		return toolinstall.Installation{}, err
	}
	if err := os.WriteFile(filepath.Join(temporary, "host"), []byte(request.Host+"\n"), 0o600); err != nil {
		return toolinstall.Installation{}, err
	}
	if err := copyFile(filepath.Join(temporary, executableName), sourceExecutable, mode.Perm()|0o500); err != nil {
		return toolinstall.Installation{}, err
	}
	if err := ctx.Err(); err != nil {
		return toolinstall.Installation{}, err
	}
	// Revalida a cópia que será ativada, não apenas a fonte.
	copiedRaw, err := os.ReadFile(filepath.Join(temporary, "manifest.yaml"))
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if _, err := toolmanifest.ParseAndValidate(copiedRaw, toolmanifest.ValidationOptions{Categories: s.categories, Target: s.target}); err != nil {
		return toolinstall.Installation{}, err
	}
	if _, _, err := inspectArtifact(filepath.Join(temporary, executableName)); err != nil {
		return toolinstall.Installation{}, err
	}
	if err := os.Rename(temporary, versionDir); err != nil {
		return toolinstall.Installation{}, fmt.Errorf("ativar diretório versionado: %w", err)
	}

	previous, _ := readPointer(idDir, "active")
	if previous != "" && previous != manifest.Version {
		if err := writePointer(idDir, "previous", previous); err != nil {
			return toolinstall.Installation{}, err
		}
	}
	if err := writePointer(idDir, "active", manifest.Version); err != nil {
		return toolinstall.Installation{}, err
	}
	return toolinstall.Installation{
		Host: request.Host, ID: manifest.ID, Version: manifest.Version, PreviousVersion: previous,
		SHA256: checksum, Path: versionDir,
	}, nil
}

func matchesExpectation(manifest toolmanifest.Manifest, expected toolinstall.ManifestExpectation) error {
	mismatch := func(field string) error {
		return fmt.Errorf("manifest empacotado diverge do índice no campo %s", field)
	}
	switch {
	case manifest.ID != expected.ID:
		return mismatch("id")
	case manifest.Version != expected.Version:
		return mismatch("version")
	case manifest.Name != expected.Name:
		return mismatch("name")
	case manifest.Summary != expected.Summary:
		return mismatch("summary")
	case manifest.Detail != expected.Detail:
		return mismatch("detail")
	case manifest.Category != expected.Category:
		return mismatch("category")
	case manifest.Risk != expected.Risk:
		return mismatch("risk")
	case manifest.Glyph != expected.Glyph:
		return mismatch("glyph")
	case manifest.Runtime.Protocol.Min != expected.ProtocolMin || manifest.Runtime.Protocol.Max != expected.ProtocolMax:
		return mismatch("runtime.protocol")
	case manifest.Permissions.Network != expected.Network:
		return mismatch("permissions.network")
	case manifest.Permissions.Subprocess != expected.Subprocess:
		return mismatch("permissions.subprocess")
	case !sameStrings(manifest.Permissions.Filesystem.Read, expected.FilesystemRead):
		return mismatch("permissions.filesystem.read")
	case !sameStrings(manifest.Permissions.Filesystem.Write, expected.FilesystemWrite):
		return mismatch("permissions.filesystem.write")
	// Um índice que não fala sobre workingDir é anterior ao campo, não um
	// índice em conflito: exigir o campo faria toda ferramenta de publicação
	// existente parar de funcionar. A divergência só existe quando o índice
	// declara um valor e o pacote traz outro.
	case expected.WorkingDir != "" && manifest.Permissions.WorkingDir != expected.WorkingDir:
		return mismatch("permissions.workingDir")
	default:
		return nil
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func (s *Store) List(ctx context.Context) ([]toolinstall.Installed, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.list(ctx)
}

func (s *Store) list(ctx context.Context) ([]toolinstall.Installed, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	installed := make([]toolinstall.Installed, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		idDir := filepath.Join(s.root, entry.Name())
		active, err := readPointer(idDir, "active")
		if err != nil {
			continue
		}
		previous, _ := readPointer(idDir, "previous")
		host, _ := readHost(filepath.Join(idDir, active))
		installed = append(installed, toolinstall.Installed{
			Host: host, ID: entry.Name(), ActiveVersion: active, PreviousVersion: previous,
		})
	}
	sort.Slice(installed, func(i, j int) bool { return installed[i].ID < installed[j].ID })
	return installed, nil
}

func (s *Store) Rollback(ctx context.Context, id string) (toolinstall.Installation, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := safeID(id); err != nil {
		return toolinstall.Installation{}, err
	}
	idDir := filepath.Join(s.root, id)
	active, err := readPointer(idDir, "active")
	if err != nil {
		return toolinstall.Installation{}, err
	}
	previous, err := readPointer(idDir, "previous")
	if err != nil || previous == "" {
		return toolinstall.Installation{}, fmt.Errorf("%s não tem versão anterior", id)
	}
	if err := ctx.Err(); err != nil {
		return toolinstall.Installation{}, err
	}
	manifest, executable, err := s.validateInstalled(idDir, id, previous)
	if err != nil {
		return toolinstall.Installation{}, fmt.Errorf("rollback recusado: %w", err)
	}
	checksum, _, err := inspectArtifact(executable)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if err := writePointer(idDir, "previous", active); err != nil {
		return toolinstall.Installation{}, err
	}
	if err := writePointer(idDir, "active", previous); err != nil {
		// Restaura o ponteiro auxiliar; active ainda não mudou.
		_ = writePointer(idDir, "previous", previous)
		return toolinstall.Installation{}, err
	}
	return toolinstall.Installation{
		Host: installedHost(filepath.Join(idDir, previous)), ID: id,
		Version: manifest.Version, PreviousVersion: active,
		SHA256: checksum, Path: filepath.Join(idDir, previous),
	}, nil
}

func (s *Store) Remove(ctx context.Context, id string) (toolinstall.Removal, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := safeID(id); err != nil {
		return toolinstall.Removal{}, err
	}
	if err := ctx.Err(); err != nil {
		return toolinstall.Removal{}, err
	}
	source := filepath.Join(s.root, id)
	if _, err := os.Stat(source); err != nil {
		return toolinstall.Removal{}, err
	}
	trash := filepath.Join(s.root, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return toolinstall.Removal{}, err
	}
	destination := filepath.Join(trash, fmt.Sprintf("%s-%d", id, s.now().UnixNano()))
	if err := os.Rename(source, destination); err != nil {
		return toolinstall.Removal{}, err
	}
	return toolinstall.Removal{ID: id, RecoveryDir: destination}, nil
}

// Reconcile monta o estado seguinte fora do diretório ativo e faz a troca
// somente depois que todos os manifests e executáveis foram revalidados.
// Assim uma falha no último pacote não deixa metade do snapshot aplicada.
func (s *Store) Reconcile(ctx context.Context, requests []toolinstall.InstallRequest) (toolinstall.Reconciliation, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	current, err := s.list(ctx)
	if err != nil {
		return toolinstall.Reconciliation{}, err
	}
	if err := validateReconcileRequests(requests); err != nil {
		return toolinstall.Reconciliation{}, err
	}
	changed := reconciliationChanges(current, requests)
	if changed == 0 {
		return toolinstall.Reconciliation{}, nil
	}

	parent := filepath.Dir(s.root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return toolinstall.Reconciliation{}, err
	}
	nextRoot, err := os.MkdirTemp(parent, "."+filepath.Base(s.root)+"-next-")
	if err != nil {
		return toolinstall.Reconciliation{}, err
	}
	cleanupNext := true
	defer func() {
		if cleanupNext {
			_ = os.RemoveAll(nextRoot)
		}
	}()

	next := New(nextRoot, categoriesFromSet(s.categories), s.target, s.now)
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return toolinstall.Reconciliation{}, err
		}
		if request.SourceDir == "" {
			request.SourceDir = filepath.Join(s.root, request.ExpectedID, request.ExpectedVersion)
		}
		if _, err := next.install(ctx, request); err != nil {
			return toolinstall.Reconciliation{}, fmt.Errorf(
				"preparar %s@%s para reconciliação: %w",
				request.ExpectedID, request.ExpectedVersion, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return toolinstall.Reconciliation{}, err
	}

	trashRoot := filepath.Join(parent, ".trash")
	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		return toolinstall.Reconciliation{}, err
	}
	recoveryDir, err := reservePath(trashRoot, filepath.Base(s.root)+"-previous-")
	if err != nil {
		return toolinstall.Reconciliation{}, err
	}
	_, statErr := os.Stat(s.root)
	previousPresent := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return toolinstall.Reconciliation{}, statErr
	}
	if previousPresent {
		if err := os.Rename(s.root, recoveryDir); err != nil {
			return toolinstall.Reconciliation{}, fmt.Errorf("guardar estado anterior das tools: %w", err)
		}
	}
	if err := os.Rename(nextRoot, s.root); err != nil {
		if previousPresent {
			_ = os.Rename(recoveryDir, s.root)
		}
		return toolinstall.Reconciliation{}, fmt.Errorf("ativar estado reconciliado das tools: %w", err)
	}
	cleanupNext = false
	return toolinstall.Reconciliation{
		Changed: changed, RecoveryDir: recoveryDir, PreviousPresent: previousPresent,
	}, nil
}

// Restore desfaz uma reconciliação inteira. O estado substituído também fica
// recuperável no mesmo caminho, em vez de ser apagado durante o rollback.
func (s *Store) Restore(ctx context.Context, reconciliation toolinstall.Reconciliation) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if reconciliation.Changed == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reconciliation.PreviousPresent {
		if err := os.Rename(s.root, reconciliation.RecoveryDir); err != nil {
			return fmt.Errorf("guardar estado desfeito das tools: %w", err)
		}
		return nil
	}

	discarded, err := reservePath(
		filepath.Dir(reconciliation.RecoveryDir), filepath.Base(s.root)+"-discarded-")
	if err != nil {
		return err
	}
	if err := os.Rename(s.root, discarded); err != nil {
		return err
	}
	if err := os.Rename(reconciliation.RecoveryDir, s.root); err != nil {
		_ = os.Rename(discarded, s.root)
		return fmt.Errorf("restaurar estado anterior das tools: %w", err)
	}
	if err := os.Rename(discarded, reconciliation.RecoveryDir); err != nil {
		return fmt.Errorf("estado anterior restaurado, mas o estado desfeito não foi arquivado: %w", err)
	}
	return nil
}

func validateReconcileRequests(requests []toolinstall.InstallRequest) error {
	seen := make(map[string]bool, len(requests))
	for _, request := range requests {
		if err := safeID(request.Host); err != nil {
			return fmt.Errorf("host da reconciliação: %w", err)
		}
		if err := safeID(request.ExpectedID); err != nil {
			return fmt.Errorf("ID da reconciliação: %w", err)
		}
		if strings.TrimSpace(request.ExpectedVersion) == "" || filepath.Base(request.ExpectedVersion) != request.ExpectedVersion || strings.ContainsAny(request.ExpectedVersion, `/\\`) {
			return errors.New("versão da reconciliação é insegura")
		}
		if seen[request.ExpectedID] {
			return fmt.Errorf("tool duplicada na reconciliação: %s", request.ExpectedID)
		}
		seen[request.ExpectedID] = true
	}
	return nil
}

func reconciliationChanges(current []toolinstall.Installed, requests []toolinstall.InstallRequest) int {
	left := make(map[string]string, len(current))
	for _, item := range current {
		left[item.ID] = item.Host + "\x00" + item.ActiveVersion
	}
	right := make(map[string]string, len(requests))
	for _, request := range requests {
		right[request.ExpectedID] = request.Host + "\x00" + request.ExpectedVersion
	}
	changed := 0
	for id, value := range left {
		if right[id] != value {
			changed++
		}
	}
	for id, value := range right {
		if left[id] == "" && value != "" {
			changed++
		}
	}
	return changed
}

func categoriesFromSet(values map[domain.CategoryID]bool) []domain.Category {
	categories := make([]domain.Category, 0, len(values))
	for id := range values {
		categories = append(categories, domain.Category{ID: id})
	}
	return categories
}

func reservePath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) validateInstalled(idDir, id, version string) (toolmanifest.Manifest, string, error) {
	versionDir := filepath.Join(idDir, version)
	raw, err := os.ReadFile(filepath.Join(versionDir, "manifest.yaml"))
	if err != nil {
		return toolmanifest.Manifest{}, "", err
	}
	manifest, err := toolmanifest.ParseAndValidate(raw, toolmanifest.ValidationOptions{Categories: s.categories, Target: s.target})
	if err != nil {
		return toolmanifest.Manifest{}, "", err
	}
	if manifest.ID != id || manifest.Version != version || !manifest.Supports(s.target) {
		return toolmanifest.Manifest{}, "", errors.New("manifest não corresponde à versão instalada ou à plataforma")
	}
	return manifest, filepath.Join(versionDir, manifest.ExecutableName(s.target)), nil
}

func inspectArtifact(path string) (string, os.FileMode, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("abrir artefato: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, errors.New("artefato não é arquivo regular")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), info.Mode(), nil
}

func copyFile(destination, source string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func readPointer(idDir, name string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(idDir, name))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("ponteiro %s inválido", name)
	}
	return value, nil
}

func writePointer(idDir, name, value string) error {
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("valor inseguro para %s", name)
	}
	temporary, err := os.CreateTemp(idDir, "."+name+"-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(value + "\n"); err != nil {
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
	return os.Rename(temporaryName, filepath.Join(idDir, name))
}

func readHost(versionDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(versionDir, "host"))
	if err != nil {
		return "", err
	}
	host := strings.TrimSpace(string(raw))
	if err := safeID(host); err != nil {
		return "", fmt.Errorf("host de instalação inválido: %w", err)
	}
	return host, nil
}

func installedHost(versionDir string) string {
	host, _ := readHost(versionDir)
	return host
}

func safeID(id string) error {
	if !validID.MatchString(id) || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return errors.New("ID de tool inseguro")
	}
	return nil
}
