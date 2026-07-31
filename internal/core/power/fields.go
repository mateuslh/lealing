package power

// Feature é um conjunto de configurações que um Manager sabe ler e escrever.
//
// Existe porque as plataformas não expõem o mesmo painel: o pmset tem onze
// chaves, o powercfg cobre três delas e nenhuma das outras. Sem este
// contrato, a tela mostraria no Windows um interruptor de Power Nap que não
// liga nada — e o diff de "alterações pendentes" nunca fecharia, porque o
// sistema devolveria sempre o valor que não pode guardar.
//
// Bitmask porque é sempre usado como conjunto: cada constante abaixo é o
// conjunto de um elemento.
type Feature uint16

const (
	// FeatureSleep é o tempo até o sistema dormir.
	FeatureSleep Feature = 1 << iota
	// FeatureDisplaySleep é o tempo até a tela desligar.
	FeatureDisplaySleep
	// FeatureDiskSleep é o tempo até o disco parar.
	FeatureDiskSleep
	// FeaturePowerNap é a atualização em segundo plano durante o sono.
	FeaturePowerNap
	// FeatureLowPowerMode é o modo de baixo consumo.
	FeatureLowPowerMode
	// FeatureReduceBrightness escurece a tela ao sair da tomada.
	FeatureReduceBrightness
	// FeatureWakeOnNetwork acorda a máquina por pacote de rede.
	FeatureWakeOnNetwork
	// FeatureTCPKeepAlive mantém conexões vivas durante o sono.
	FeatureTCPKeepAlive
	// FeatureTTYsKeepAwake impede o sono enquanto há sessão de terminal.
	FeatureTTYsKeepAwake
	// FeatureStandby é o sono profundo agendado.
	FeatureStandby
	// FeatureHibernateMode escolhe entre RAM, disco ou ambos.
	FeatureHibernateMode
	// FeaturePasswordless é a dispensa de senha para aplicar mudanças. Não é
	// um campo do perfil: é a capacidade de instalar a regra que evita a
	// autenticação a cada aplicação.
	FeaturePasswordless
)

// AllFeatures é o painel completo, que hoje só o macOS oferece.
const AllFeatures = FeatureSleep | FeatureDisplaySleep | FeatureDiskSleep |
	FeaturePowerNap | FeatureLowPowerMode | FeatureReduceBrightness |
	FeatureWakeOnNetwork | FeatureTCPKeepAlive | FeatureTTYsKeepAwake |
	FeatureStandby | FeatureHibernateMode | FeaturePasswordless

// Has informa se todos os bits de want estão presentes.
func (f Feature) Has(want Feature) bool { return want != 0 && f&want == want }

// Scope diz em quais fontes de alimentação um campo se aplica. O pmset recusa
// a linha inteira se receber uma chave que não vale para a fonte, e mostrar um
// controle que não faz nada seria pior ainda.
type Scope uint8

const (
	// ScopeBoth vale nas duas fontes.
	ScopeBoth Scope = iota
	// ScopeBatteryOnly só vale na bateria.
	ScopeBatteryOnly
	// ScopeACOnly só vale no carregador.
	ScopeACOnly
)

// Control determina o tipo de controle que edita o campo. É a única
// concessão do núcleo à apresentação, e existe para que a tela não precise
// inferir o controle a partir do nome do campo.
type Control uint8

const (
	// ControlMinutes é uma escala de tempo em minutos.
	ControlMinutes Control = iota
	// ControlToggle é um interruptor.
	ControlToggle
	// ControlHibernate cicla entre os modos de hibernação válidos.
	ControlHibernate
)

// Field descreve uma configuração editável do perfil.
//
// O acesso é por ponteiro derivado do Profile em vez de switch por nome: com
// onze campos, um switch apareceria em toda operação (ler, ajustar, alternar,
// mesclar) e as cópias divergiriam na primeira vez que alguém adicionasse uma
// opção. Esta tabela é a fonte única — quem precisa percorrer campos percorre
// Fields(), não uma lista própria.
type Field struct {
	Label   string
	Hint    string
	Feature Feature
	Scope   Scope
	Control Control

	IntAt  func(*Profile) *int
	BoolAt func(*Profile) *bool
}

// AppliesTo informa se o campo vale para a fonte.
func (f Field) AppliesTo(src Source) bool {
	switch f.Scope {
	case ScopeACOnly:
		return src == AC
	case ScopeBatteryOnly:
		return src == Battery
	default:
		return true
	}
}

// Step move o valor do campo na direção indicada. Toggles viram ligado com
// delta positivo e desligado com negativo, que é o gesto das setas.
func (f Field) Step(p *Profile, delta int) {
	switch f.Control {
	case ControlMinutes:
		ptr := f.IntAt(p)
		*ptr = StepMinutes(*ptr, delta)
	case ControlHibernate:
		ptr := f.IntAt(p)
		*ptr = StepHibernate(*ptr, delta)
	case ControlToggle:
		*f.BoolAt(p) = delta > 0
	}
}

// Toggle é o gesto de "ativar": inverte booleanos e avança escalas.
func (f Field) Toggle(p *Profile) {
	if f.Control == ControlToggle {
		ptr := f.BoolAt(p)
		*ptr = !*ptr
		return
	}
	f.Step(p, 1)
}

// Equal compara este campo entre dois perfis.
func (f Field) Equal(a, b *Profile) bool {
	if f.Control == ControlToggle {
		return *f.BoolAt(a) == *f.BoolAt(b)
	}
	return *f.IntAt(a) == *f.IntAt(b)
}

// fields é a ordem em que a tela apresenta as configurações: primeiro o que o
// usuário mexe todo dia, depois o avançado.
var fields = []Field{
	{
		Label: "Dormir", Hint: "sistema", Control: ControlMinutes, Feature: FeatureSleep,
		IntAt: func(p *Profile) *int { return &p.Sleep },
	},
	{
		Label: "Desligar a tela", Control: ControlMinutes, Feature: FeatureDisplaySleep,
		IntAt: func(p *Profile) *int { return &p.DisplaySleep },
	},
	{
		Label: "Dormir o disco", Control: ControlMinutes, Feature: FeatureDiskSleep,
		IntAt: func(p *Profile) *int { return &p.DiskSleep },
	},
	{
		Label: "Power Nap", Hint: "atualiza dormindo", Control: ControlToggle, Feature: FeaturePowerNap,
		BoolAt: func(p *Profile) *bool { return &p.PowerNap },
	},
	{
		Label: "Modo de baixo consumo", Control: ControlToggle, Feature: FeatureLowPowerMode,
		BoolAt: func(p *Profile) *bool { return &p.LowPowerMode },
	},
	{
		Label: "Reduzir brilho", Hint: "só na bateria", Control: ControlToggle,
		Feature: FeatureReduceBrightness, Scope: ScopeBatteryOnly,
		BoolAt: func(p *Profile) *bool { return &p.ReduceBrightness },
	},
	{
		Label: "Acordar pela rede", Hint: "só no carregador", Control: ControlToggle,
		Feature: FeatureWakeOnNetwork, Scope: ScopeACOnly,
		BoolAt: func(p *Profile) *bool { return &p.WakeOnNetwork },
	},
	{
		Label: "Manter TCP vivo", Control: ControlToggle, Feature: FeatureTCPKeepAlive,
		BoolAt: func(p *Profile) *bool { return &p.TCPKeepAlive },
	},
	{
		Label: "Terminal segura acordado", Hint: "ttyskeepawake", Control: ControlToggle,
		Feature: FeatureTTYsKeepAwake,
		BoolAt:  func(p *Profile) *bool { return &p.TTYsKeepAwake },
	},
	{
		Label: "Standby", Control: ControlToggle, Feature: FeatureStandby,
		BoolAt: func(p *Profile) *bool { return &p.Standby },
	},
	{
		Label: "Hibernação", Control: ControlHibernate, Feature: FeatureHibernateMode,
		IntAt: func(p *Profile) *int { return &p.HibernateMode },
	},
}

// Fields devolve a tabela completa, incluindo o que a plataforma atual não
// suporta. Serve à documentação e aos testes; a tela quer VisibleFields.
func Fields() []Field { return fields }

// VisibleFields devolve os campos que a fonte aceita e o Manager suporta,
// preservando a ordem.
func VisibleFields(src Source, feats Feature) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.AppliesTo(src) && feats.Has(f.Feature) {
			out = append(out, f)
		}
	}
	return out
}

// Merge copia de next apenas os campos suportados, preservando o resto de
// base.
//
// É o que mantém honesto o aviso de "alterações pendentes" em plataformas
// parciais: um preset que liga Power Nap não pode marcar a tela como alterada
// no Windows, onde Power Nap nunca chegaria ao sistema.
func Merge(base, next Settings, feats Feature) Settings {
	base.Battery = mergeProfile(base.Battery, next.Battery, Battery, feats)
	base.AC = mergeProfile(base.AC, next.AC, AC, feats)
	return base
}

func mergeProfile(base, next Profile, src Source, feats Feature) Profile {
	for _, f := range fields {
		if !f.AppliesTo(src) || !feats.Has(f.Feature) {
			continue
		}
		if f.Control == ControlToggle {
			*f.BoolAt(&base) = *f.BoolAt(&next)
			continue
		}
		*f.IntAt(&base) = *f.IntAt(&next)
	}
	return base
}

// minuteSteps é a escala usada pelas setas.
//
// Uma escala, e não incremento de um minuto: ninguém quer apertar a seta
// cinquenta e nove vezes para ir de 1 para 60, e estes são os valores que os
// painéis de energia dos dois sistemas oferecem.
var minuteSteps = []int{0, 1, 2, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180}

// StepMinutes move o valor para o próximo degrau da escala.
func StepMinutes(current, delta int) int {
	idx := 0
	for i, step := range minuteSteps {
		if step <= current {
			idx = i
		}
	}
	// Um valor fora da escala (configurado por fora) sobe para o degrau
	// seguinte em vez de saltar para trás.
	if delta > 0 && minuteSteps[idx] < current {
		return minuteSteps[min(idx+1, len(minuteSteps)-1)]
	}
	return minuteSteps[min(max(idx+delta, 0), len(minuteSteps)-1)]
}

// hibernateModes são os únicos valores que o macOS aceita.
var hibernateModes = []int{0, 3, 25}

// StepHibernate cicla entre os modos válidos.
func StepHibernate(current, delta int) int {
	idx := 1 // 3 é o padrão
	for i, mode := range hibernateModes {
		if mode == current {
			idx = i
		}
	}
	return hibernateModes[min(max(idx+delta, 0), len(hibernateModes)-1)]
}
