//go:build !darwin

package claudecli

import "github.com/mateuslh/lealing/internal/core/ccaccount"

// NewVault monta o cofre da plataforma.
//
// Fora do macOS — Windows e Linux — a CLI guarda a credencial no próprio
// ~/.claude/.credentials.json, sem cofre do sistema. Não há chaveiro a
// consultar, então o arquivo é o cofre.
func NewVault(credentialsPath string) ccaccount.Vault {
	return NewFileVault(credentialsPath)
}
