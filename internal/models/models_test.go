package models

import (
	"encoding/json"
	"testing"
)

func TestIngredientOrdering(t *testing.T) {
	jsonStr := `{"type":"ingredient","name":"Flour","order":5}`
	var i Ingredient
	if err := json.Unmarshal([]byte(jsonStr), &i); err != nil {
		t.Fatalf("Failed to unmarshal ingredient: %v", err)
	}

	if i.Order != 5 {
		t.Errorf("Expected Order to be 5, got %d", i.Order)
	}
}

func TestInstructionOrdering(t *testing.T) {
	jsonStr := `{"step":"Mix","order":10}`
	var i Instruction
	if err := json.Unmarshal([]byte(jsonStr), &i); err != nil {
		t.Fatalf("Failed to unmarshal instruction: %v", err)
	}

	if i.Order != 10 {
		t.Errorf("Expected Order to be 10, got %d", i.Order)
	}
}

func TestIngredientsListSorting(t *testing.T) {
	jsonStr := `[{"name":"Salt","order":2},{"name":"Flour","order":0},{"name":"Water","order":1}]`
	var il IngredientsList
	if err := il.Scan([]byte(jsonStr)); err != nil {
		t.Fatalf("Failed to scan ingredients list: %v", err)
	}

	if len(il) != 3 {
		t.Fatalf("Expected 3 ingredients, got %d", len(il))
	}

	if il[0].Name != "Flour" || il[0].Order != 0 {
		t.Errorf("Expected index 0 to be Flour (0), got %s (%d)", il[0].Name, il[0].Order)
	}
	if il[1].Name != "Water" || il[1].Order != 1 {
		t.Errorf("Expected index 1 to be Water (1), got %s (%d)", il[1].Name, il[1].Order)
	}
	if il[2].Name != "Salt" || il[2].Order != 2 {
		t.Errorf("Expected index 2 to be Salt (2), got %s (%d)", il[2].Name, il[2].Order)
	}
}

func TestInstructionsListSorting(t *testing.T) {
	jsonStr := `[{"step":"Eat","order":2},{"step":"Cook","order":1},{"step":"Prep","order":0}]`
	var il InstructionsList
	if err := il.Scan([]byte(jsonStr)); err != nil {
		t.Fatalf("Failed to scan instructions list: %v", err)
	}

	if len(il) != 3 {
		t.Fatalf("Expected 3 instructions, got %d", len(il))
	}

	if il[0].Step != "Prep" || il[0].Order != 0 {
		t.Errorf("Expected index 0 to be Prep (0), got %s (%d)", il[0].Step, il[0].Order)
	}
	if il[1].Step != "Cook" || il[1].Order != 1 {
		t.Errorf("Expected index 1 to be Cook (1), got %s (%d)", il[1].Step, il[1].Order)
	}
	if il[2].Step != "Eat" || il[2].Order != 2 {
		t.Errorf("Expected index 2 to be Eat (2), got %s (%d)", il[2].Step, il[2].Order)
	}
}

func TestInstructionType(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		expected string
	}{
		{
			name:     "Instruction with type",
			jsonStr:  `{"type":"instruction","step":"Mix","order":0}`,
			expected: "instruction",
		},
		{
			name:     "Header with type",
			jsonStr:  `{"type":"header","step":"Filling","order":1}`,
			expected: "header",
		},
		{
			name:     "Implicit instruction",
			jsonStr:  `{"step":"Bake","order":2}`,
			expected: "", // Default value for string if not present in JSON
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var i Instruction
			if err := json.Unmarshal([]byte(tt.jsonStr), &i); err != nil {
				t.Fatalf("Failed to unmarshal instruction: %v", err)
			}
			if i.Type != tt.expected {
				t.Errorf("Expected Type %s, got %s", tt.expected, i.Type)
			}
		})
	}
}
