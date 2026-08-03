package machine

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FileSystem oferece as operações de arquivo mais comuns com as concessões
// do handshake aplicadas antes de tocar o disco.
type FileSystem struct{ environment Environment }

// Files devolve o acesso a disco ligado a este ambiente.
func (e Environment) Files() FileSystem { return FileSystem{environment: e} }

// Open abre um arquivo concedido para leitura incremental.
func (f FileSystem) Open(name string) (*os.File, error) {
	path, err := f.environment.ResolveRead(name)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// ReadFile lê um arquivo concedido.
func (f FileSystem) ReadFile(name string) ([]byte, error) {
	path, err := f.environment.ResolveRead(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Stat consulta metadados de um caminho concedido para leitura.
func (f FileSystem) Stat(name string) (fs.FileInfo, error) {
	path, err := f.environment.ResolveRead(name)
	if err != nil {
		return nil, err
	}
	return os.Stat(path)
}

// ReadDir lista um diretório concedido para leitura.
func (f FileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	path, err := f.environment.ResolveRead(name)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

// WalkDir percorre uma árvore concedida.
func (f FileSystem) WalkDir(root string, walk fs.WalkDirFunc) error {
	path, err := f.environment.ResolveRead(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(path, walk)
}

// MkdirAll cria uma árvore dentro de uma concessão de escrita.
func (f FileSystem) MkdirAll(name string, perm fs.FileMode) error {
	path, err := f.environment.ResolveWrite(name)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}

// WriteFileAtomic grava no mesmo diretório e troca o destino somente depois
// de conteúdo, permissão e flush concluírem. Uma falha preserva o arquivo
// anterior e remove o temporário.
func (f FileSystem) WriteFileAtomic(name string, data []byte, perm fs.FileMode) (err error) {
	destination, err := f.environment.ResolveWrite(name)
	if err != nil {
		return err
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".lealing-tool-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(perm); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, destination)
}
