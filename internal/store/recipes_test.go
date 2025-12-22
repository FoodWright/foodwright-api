package store

import (
	"testing"
	"github.com/FoodWright/foodwright-api/internal/models"
)

func TestNormalizeRecipeOrder(t *testing.T) {
	ingredients := models.IngredientsList{
		{Name: "Salt", Order: 99},
		{Name: "Water", Order: -1},
	}
	instructions := models.InstructionsList{
		{Step: "Mix", Order: 5},
		{Step: "Bake", Order: 0},
	}

	normalizeRecipeOrder(ingredients, instructions)

	if ingredients[0].Order != 0 || ingredients[1].Order != 1 {
		t.Errorf("Ingredients order not normalized: got %d and %d", ingredients[0].Order, ingredients[1].Order)
	}
	if instructions[0].Order != 0 || instructions[1].Order != 1 {
		t.Errorf("Instructions order not normalized: got %d and %d", instructions[0].Order, instructions[1].Order)
	}
}
