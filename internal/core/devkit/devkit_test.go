package devkit

import "testing"

func TestDefinitionsDeclaramOitoToolsUnicas(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 8 {
		t.Fatalf("definitions = %d, quero 8", len(definitions))
	}

	ids := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		if definition.ToolID == "" || definition.Name == "" || definition.Summary == "" {
			t.Errorf("definição incompleta: %+v", definition)
		}
		if ids[definition.ToolID] {
			t.Errorf("ID duplicado: %s", definition.ToolID)
		}
		ids[definition.ToolID] = true
		if len(definition.Modes) == 0 {
			t.Errorf("%s não declara modo", definition.ToolID)
		}
	}
}

func TestValidateRequestRecusaModoInventado(t *testing.T) {
	err := ValidateRequest(Request{Tool: ToolJSON, Mode: "yaml"})
	if err == nil {
		t.Fatal("modo inventado foi aceito")
	}
}
