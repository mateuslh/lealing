// Package sysinfo é o domínio da tool "Informações do Sistema".
//
// Cada tool do lealing tem seu próprio pacote de núcleo com os tipos e a
// porta de saída que precisa. O catálogo (internal/core/domain) sabe que a
// tool existe; o que ela faz mora aqui, sem conhecer terminal nem macOS.
package sysinfo

import "context"

// Snapshot é a leitura instantânea do estado da máquina. Todos os campos são
// texto já formatado: a decisão de como apresentar um número (GB, "3d 4h")
// pertence a quem sabe o idioma, não a quem desenha a tela.
type Snapshot struct {
	OSVersion string
	HostName  string
	Model     string
	Chip      string
	Cores     string
	Memory    string
	Uptime    string

	BatteryLevel string
	BatteryState string
	// HasBattery distingue "sem bateria" (desktop) de "não consegui ler".
	HasBattery bool
}

// Unknown é o marcador exibido quando um campo não pôde ser lido.
const Unknown = "—"

// Field é um par rótulo/valor pronto para render.
type Field struct {
	Label string
	Value string
}

// Section agrupa campos relacionados sob um título.
type Section struct {
	Title  string
	Fields []Field
}

// Sections organiza o snapshot na ordem em que a tela o apresenta. Manter
// isso no núcleo garante que a mesma leitura saia igual na TUI e em qualquer
// outro adapter (um `lealing sysinfo --json`, por exemplo).
func (s Snapshot) Sections() []Section {
	battery := Section{Title: "Bateria"}
	if s.HasBattery {
		battery.Fields = []Field{
			{"Carga", s.BatteryLevel},
			{"Estado", s.BatteryState},
		}
	} else {
		battery.Fields = []Field{{"Estado", "Sem bateria — alimentado pela tomada"}}
	}

	return []Section{
		{Title: "Sistema", Fields: []Field{
			{"Sistema operacional", s.OSVersion},
			{"Nome do computador", s.HostName},
			{"Tempo ligado", s.Uptime},
		}},
		{Title: "Hardware", Fields: []Field{
			{"Modelo", s.Model},
			{"Processador", s.Chip},
			{"CPU", s.Cores},
			{"Memória", s.Memory},
		}},
		battery,
	}
}

// Inspector é a porta de saída: alguém que sabe ler a máquina.
type Inspector interface {
	Inspect(ctx context.Context) (Snapshot, error)
}
