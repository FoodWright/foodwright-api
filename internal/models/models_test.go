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
