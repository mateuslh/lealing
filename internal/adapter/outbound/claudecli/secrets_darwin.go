//go:build darwin

package claudecli

import "context"

// profilesService é o item do chaveiro onde os perfis do lealing moram. Um
// service próprio, separado do da CLI, para que apagar os perfis nunca
// esbarre na credencial ativa.
const profilesService = "lealing-claude-accounts"

// keychainSecrets guarda cada perfil como um item genérico do chaveiro, com
// o nome do perfil na conta.
type keychainSecrets struct{ kc *keychain }

var _ secretStore = (*keychainSecrets)(nil)

// newSecretStore devolve o cofre da plataforma. No macOS o diretório de
// dados não é usado: nada sensível vai para disco.
func newSecretStore(string) secretStore {
	return &keychainSecrets{kc: &keychain{service: profilesService}}
}

func (s *keychainSecrets) Get(ctx context.Context, key string) ([]byte, error) {
	return s.kc.get(ctx, key)
}

func (s *keychainSecrets) Set(ctx context.Context, key string, value []byte) error {
	return s.kc.set(ctx, key, value)
}

func (s *keychainSecrets) Delete(ctx context.Context, key string) error {
	return s.kc.delete(ctx, key)
}
