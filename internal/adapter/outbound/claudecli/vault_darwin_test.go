//go:build darwin

package claudecli

import (
	"context"
	"testing"
)

// dump é a descrição de um item como o `security` a imprime. O teste usa uma
// saída fixa porque a alternativa — ler o chaveiro da máquina — não roda em
// CI e depende do que o usuário tem instalado.
const dump = `keychain: "/Users/alguem/Library/Keychains/login.keychain-db"
version: 512
class: "genp"
attributes:
    0x00000007 <blob>="Claude Code-credentials"
    0x00000008 <blob>=<NULL>
    "acct"<blob>="alguem"
    "cdat"<timedate>=0x32303236303730313134353935395A00  "20260701145959Z\000"
    "svce"<blob>="Claude Code-credentials"
    "type"<uint32>=<NULL>
`

func TestParseKeychainAccount(t *testing.T) {
	if got := ParseKeychainAccount(dump); got != "alguem" {
		t.Errorf("conta = %q, quero “alguem”", got)
	}
	if got := ParseKeychainAccount("item não encontrado"); got != "" {
		t.Errorf("saída sem acct devia render vazio, veio %q", got)
	}
	// Nome com espaço é o caso de quem tem o usuário do sistema assim; sem o
	// recorte pelo último aspas, viria truncado.
	comEspaco := `    "acct"<blob>="nome com espaço"`
	if got := ParseKeychainAccount(comEspaco); got != "nome com espaço" {
		t.Errorf("conta = %q", got)
	}
}

// TestSecurityRodaForaDaSessaoDeTerminal trava o detalhe que fez a tool
// pendurar a TUI: com terminal controlador, o `security` abre /dev/tty,
// escreve "password data for new item:" por cima do frame e espera uma
// digitação que a TUI nunca entrega.
func TestSecurityRodaForaDaSessaoDeTerminal(t *testing.T) {
	cmd := securityCmd(context.Background(), "find-generic-password", "-s", "qualquer")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Error("chamada ao `security` sem Setsid: o prompt voltaria a travar a tela")
	}
}
