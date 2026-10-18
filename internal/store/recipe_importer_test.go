package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nethtml "golang.org/x/net/html"
)

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		valid    bool
	}{
		{"PT15M", 15, true},
		{"PT1H", 60, true},
		{"PT1H30M", 90, true},
		{"PT2H15M", 135, true},
		{"P0DT0H45M", 45, true},
		{"pt30m", 30, true},
		{"PT45M30S", 46, true},
		{"45", 45, true},
		{"", 0, false},
		{"invalid", 0, false},
	}

	for _, tt := range tests {
		res := parseISO8601Duration(tt.input)
		if res.Valid != tt.valid {
			t.Errorf("parseISO8601Duration(%q) valid = %v, expected %v", tt.input, res.Valid, tt.valid)
		}
		if tt.valid && res.Int64 != tt.expected {
			t.Errorf("parseISO8601Duration(%q) = %d, expected %d", tt.input, res.Int64, tt.expected)
		}
	}
}

func TestParseIngredientString(t *testing.T) {
	tests := []struct {
		raw         string
		expectedType string
		expectedQty string
		expectedUnit string
		expectedName string
	}{
		{
			raw:         "1 1/2 cups all-purpose flour",
			expectedType: "ingredient",
			expectedQty: "1 1/2",
			expectedUnit: "cup",
			expectedName: "all-purpose flour",
		},
		{
			raw:         "2 tablespoons unsalted butter",
			expectedType: "ingredient",
			expectedQty: "2",
			expectedUnit: "tbsp",
			expectedName: "unsalted butter",
		},
		{
			raw:         "½ tsp salt",
			expectedType: "ingredient",
			expectedQty: "1/2",
			expectedUnit: "tsp",
			expectedName: "salt",
		},
		{
			raw:         "3 large eggs",
			expectedType: "ingredient",
			expectedQty: "3",
			expectedUnit: "each",
			expectedName: "large eggs",
		},
		{
			raw:         "For the Crust:",
			expectedType: "header",
			expectedQty: "",
			expectedUnit: "each",
			expectedName: "Crust",
		},
		{
			raw:         "--- Filling ---",
			expectedType: "header",
			expectedQty: "",
			expectedUnit: "each",
			expectedName: "Filling",
		},
	}

	for _, tt := range tests {
		ing := parseIngredientString(tt.raw, 0)
		if ing.Type != tt.expectedType {
			t.Errorf("parseIngredientString(%q) type = %q, expected %q", tt.raw, ing.Type, tt.expectedType)
		}
		if ing.Type == "header" {
			if ing.Name != tt.expectedName {
				t.Errorf("parseIngredientString(%q) header name = %q, expected %q", tt.raw, ing.Name, tt.expectedName)
			}
		} else {
			if ing.QuantityStr != tt.expectedQty {
				t.Errorf("parseIngredientString(%q) qty = %q, expected %q", tt.raw, ing.QuantityStr, tt.expectedQty)
			}
			if ing.Unit != tt.expectedUnit {
				t.Errorf("parseIngredientString(%q) unit = %q, expected %q", tt.raw, ing.Unit, tt.expectedUnit)
			}
			if ing.Name != tt.expectedName {
				t.Errorf("parseIngredientString(%q) name = %q, expected %q", tt.raw, ing.Name, tt.expectedName)
			}
		}
	}
}

func TestExtractInstructions(t *testing.T) {
	// 1. Array of strings
	v1 := []interface{}{"Step 1: Prep", "Step 2: Cook"}
	ins1 := extractInstructions(v1)
	if len(ins1) != 2 || ins1[0].Step != "Step 1: Prep" || ins1[1].Step != "Step 2: Cook" {
		t.Fatalf("extractInstructions string array failed: %#v", ins1)
	}

	// 2. HowToStep objects
	v2 := []interface{}{
		map[string]interface{}{"@type": "HowToStep", "text": "Mix well."},
		map[string]interface{}{"@type": "HowToStep", "text": "Bake at 350F."},
	}
	ins2 := extractInstructions(v2)
	if len(ins2) != 2 || ins2[0].Step != "Mix well." || ins2[1].Step != "Bake at 350F." {
		t.Fatalf("extractInstructions HowToStep failed: %#v", ins2)
	}

	// 3. HowToSection with nested steps
	v3 := []interface{}{
		map[string]interface{}{
			"@type": "HowToSection",
			"name":  "Crust",
			"itemListElement": []interface{}{
				map[string]interface{}{"@type": "HowToStep", "text": "Crush graham crackers."},
				map[string]interface{}{"@type": "HowToStep", "text": "Press into pan."},
			},
		},
		map[string]interface{}{
			"@type": "HowToSection",
			"name":  "Filling",
			"itemListElement": []interface{}{
				map[string]interface{}{"@type": "HowToStep", "text": "Beat cream cheese."},
			},
		},
	}
	ins3 := extractInstructions(v3)
	if len(ins3) != 5 {
		t.Fatalf("Expected 5 items (2 headers + 3 steps), got %d: %#v", len(ins3), ins3)
	}
	if ins3[0].Type != "header" || ins3[0].Step != "Crust" {
		t.Errorf("Expected first item to be header Crust, got %#v", ins3[0])
	}
	if ins3[1].Type != "instruction" || ins3[1].Step != "Crush graham crackers." {
		t.Errorf("Expected second item to be instruction, got %#v", ins3[1])
	}
	if ins3[3].Type != "header" || ins3[3].Step != "Filling" {
		t.Errorf("Expected fourth item to be header Filling, got %#v", ins3[3])
	}
}

func TestFindAllJsonLdAndFindRecipe(t *testing.T) {
	htmlContent := `
	<!DOCTYPE html>
	<html>
	<head>
		<script type="application/ld+json">
		{
			"@context": "https://schema.org",
			"@type": "WebSite",
			"name": "Food Blog"
		}
		</script>
		<script type="application/ld+json">
		{
			"@context": "https://schema.org",
			"@graph": [
				{
					"@type": "WebPage",
					"name": "Page title"
				},
				{
					"@type": "Recipe",
					"name": "Classic Chocolate Chip Cookies",
					"description": "The best cookies ever &amp; easy to make.",
					"prepTime": "PT15M",
					"cookTime": "PT10M",
					"recipeYield": "24 cookies",
					"recipeIngredient": [
						"2 1/4 cups all-purpose flour",
						"1 cup butter, softened",
						"3/4 cup granulated sugar",
						"2 large eggs"
					],
					"recipeInstructions": [
						{"@type": "HowToStep", "text": "Preheat oven to 375 degrees F."},
						{"@type": "HowToStep", "text": "Combine flour and baking soda."}
					]
				}
			]
		}
		</script>
	</head>
	<body></body>
	</html>
	`

	doc, err := nethtml.Parse(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	scripts := findAllJsonLd(doc)
	if len(scripts) != 2 {
		t.Fatalf("Expected 2 JSON-LD scripts, got %d", len(scripts))
	}

	var schema SchemaRecipe
	var found bool
	for _, s := range scripts {
		if findRecipeInJson(json.RawMessage(s), &schema) {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Failed to find Recipe schema in JSON-LD")
	}

	recipe := buildRecipeFromSchema(schema, "https://example.com/cookies")
	if recipe.Title != "Classic Chocolate Chip Cookies" {
		t.Errorf("Expected title 'Classic Chocolate Chip Cookies', got %q", recipe.Title)
	}
	if recipe.Description != "The best cookies ever & easy to make." {
		t.Errorf("Expected unescaped description, got %q", recipe.Description)
	}
	if !recipe.PrepTimeMinutes.Valid || recipe.PrepTimeMinutes.Int64 != 15 {
		t.Errorf("Expected prep time 15, got %#v", recipe.PrepTimeMinutes)
	}
	if !recipe.CookTimeMinutes.Valid || recipe.CookTimeMinutes.Int64 != 10 {
		t.Errorf("Expected cook time 10, got %#v", recipe.CookTimeMinutes)
	}
	if !recipe.Servings.Valid || recipe.Servings.String != "24 cookies" {
		t.Errorf("Expected servings '24 cookies', got %#v", recipe.Servings)
	}
	if len(recipe.Ingredients) != 4 {
		t.Fatalf("Expected 4 ingredients, got %d", len(recipe.Ingredients))
	}
	if recipe.Ingredients[0].QuantityStr != "2 1/4" || recipe.Ingredients[0].Unit != "cup" {
		t.Errorf("Unexpected first ingredient: %#v", recipe.Ingredients[0])
	}
	if len(recipe.Instructions) != 2 {
		t.Fatalf("Expected 2 instructions, got %d", len(recipe.Instructions))
	}
}

func TestFetchWithFallback(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		ua := r.Header.Get("User-Agent")
		if strings.Contains(ua, "Firefox") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
			return
		}
		if strings.Contains(ua, "facebookexternalhit") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK from FacebookBot"))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	resp, err := fetchWithFallback(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("fetchWithFallback failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts (fallback on 403), got %d", attempts)
	}
}

