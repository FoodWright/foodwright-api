package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
	nethtml "golang.org/x/net/html"
)

// SchemaRecipe is used for unmarshaling recipe JSON-LD from schema.org
type SchemaRecipe struct {
	Type               any `json:"@type"`
	Name               any `json:"name"`
	Description        any `json:"description"`
	RecipeIngredient   any `json:"recipeIngredient"`
	RecipeInstructions any `json:"recipeInstructions"`
	PrepTime           any `json:"prepTime"`
	CookTime           any `json:"cookTime"`
	TotalTime          any `json:"totalTime"`
	RecipeYield        any `json:"recipeYield"`
}

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
var durationRegex = regexp.MustCompile(`(?i)P(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
var ingredientRegex = regexp.MustCompile(`^\s*([0-9/.\s-]+)\s*(cup|cups|c\.|oz|ounce|ounces|lb|lbs|pound|pounds|g|grams|gram|kg|kilograms|kilogram|ml|milliliter|milliliters|l|liter|liters|tsp|teaspoon|teaspoons|tbsp|tablespoon|tablespoons|tbs|tb|fl oz|fluid ounce|fluid ounces|pint|pints|pt|quart|quarts|qt|gallon|gallons|gal|each|pinch|dash)?\b\s*(.*)$`)

func cleanText(s string) string {
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}

func normalizeFractions(s string) string {
	replacements := map[string]string{
		"½": " 1/2",
		"⅓": " 1/3",
		"⅔": " 2/3",
		"¼": " 1/4",
		"¾": " 3/4",
		"⅛": " 1/8",
		"⅜": " 3/8",
		"⅝": " 5/8",
		"⅞": " 7/8",
	}
	for k, v := range replacements {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func extractString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	case map[string]interface{}:
		if s, ok := v["text"].(string); ok {
			return strings.TrimSpace(s)
		}
		if s, ok := v["name"].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func extractYieldString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return strconv.Itoa(v)
	case []interface{}:
		if len(v) > 0 {
			return extractYieldString(v[0])
		}
	}
	return ""
}

func extractIngredientStrings(val any) []string {
	if val == nil {
		return nil
	}
	var results []string
	switch v := val.(type) {
	case []interface{}:
		for _, item := range v {
			switch it := item.(type) {
			case string:
				s := strings.TrimSpace(it)
				if s != "" {
					results = append(results, s)
				}
			case map[string]interface{}:
				if text, ok := it["text"].(string); ok && strings.TrimSpace(text) != "" {
					results = append(results, strings.TrimSpace(text))
				} else if name, ok := it["name"].(string); ok && strings.TrimSpace(name) != "" {
					results = append(results, strings.TrimSpace(name))
				}
			}
		}
	case string:
		for _, line := range strings.Split(v, "\n") {
			s := strings.TrimSpace(line)
			if s != "" {
				results = append(results, s)
			}
		}
	}
	return results
}

func extractStepText(m map[string]interface{}) string {
	if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	if name, ok := m["name"].(string); ok && strings.TrimSpace(name) != "" {
		return name
	}
	return ""
}

func extractInstructions(val any) []models.Instruction {
	if val == nil {
		return nil
	}
	var instructions []models.Instruction
	var order int

	addStep := func(stepType, text string) {
		text = cleanText(text)
		if text == "" {
			return
		}
		instructions = append(instructions, models.Instruction{
			Type:  stepType,
			Step:  text,
			Order: order,
		})
		order++
	}

	switch v := val.(type) {
	case string:
		for _, line := range strings.Split(v, "\n") {
			addStep("instruction", line)
		}
	case []interface{}:
		for _, item := range v {
			switch step := item.(type) {
			case string:
				addStep("instruction", step)
			case map[string]interface{}:
				stepType, _ := step["@type"].(string)
				if strings.EqualFold(stepType, "HowToSection") {
					name, _ := step["name"].(string)
					if name != "" {
						addStep("header", name)
					}
					if itemList, ok := step["itemListElement"].([]interface{}); ok {
						for _, subItem := range itemList {
							switch sub := subItem.(type) {
							case string:
								addStep("instruction", sub)
							case map[string]interface{}:
								t := extractStepText(sub)
								addStep("instruction", t)
							}
						}
					}
				} else {
					t := extractStepText(step)
					addStep("instruction", t)
				}
			}
		}
	}
	return instructions
}

func normalizeUnit(unit string) string {
	switch unit {
	case "cup", "cups", "c.":
		return "cup"
	case "tbsp", "tablespoon", "tablespoons", "tbs", "tb":
		return "tbsp"
	case "tsp", "teaspoon", "teaspoons":
		return "tsp"
	case "fl oz", "fluid ounce", "fluid ounces":
		return "fl oz"
	case "pint", "pints", "pt":
		return "pint"
	case "quart", "quarts", "qt":
		return "quart"
	case "gallon", "gallons", "gal":
		return "gallon"
	case "oz", "ounce", "ounces":
		return "oz"
	case "lb", "lbs", "pound", "pounds":
		return "lb"
	case "g", "gram", "grams":
		return "g"
	case "kg", "kilogram", "kilograms":
		return "kg"
	case "ml", "milliliter", "milliliters":
		return "ml"
	case "l", "liter", "liters":
		return "l"
	default:
		return "each"
	}
}

func parseIngredientString(raw string, order int) models.Ingredient {
	raw = cleanText(raw)
	raw = normalizeFractions(raw)
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, "For the") && strings.HasSuffix(raw, ":") {
		return models.Ingredient{
			Type:  "header",
			Name:  strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "For the"), ":")),
			Order: order,
		}
	}
	if (strings.HasPrefix(raw, "---") && strings.HasSuffix(raw, "---")) ||
		(!strings.ContainsAny(raw, "0123456789") && strings.HasSuffix(raw, ":")) {
		return models.Ingredient{
			Type:  "header",
			Name:  strings.Trim(raw, " -:"),
			Order: order,
		}
	}

	matches := ingredientRegex.FindStringSubmatch(raw)
	if len(matches) == 4 && strings.TrimSpace(matches[1]) != "" {
		unit := normalizeUnit(strings.ToLower(strings.TrimSpace(matches[2])))
		name := strings.TrimSpace(matches[3])
		if name == "" && unit != "each" {
			name = matches[2]
			unit = "each"
		}
		return models.Ingredient{
			Type:        "ingredient",
			QuantityStr: strings.TrimSpace(matches[1]),
			Unit:        unit,
			Name:        name,
			Order:       order,
		}
	}

	return models.Ingredient{
		Type:        "ingredient",
		QuantityStr: "",
		Unit:        "each",
		Name:        raw,
		Order:       order,
	}
}

func parseISO8601Duration(duration string) sql.NullInt64 {
	duration = strings.TrimSpace(duration)
	if duration == "" {
		return sql.NullInt64{Valid: false}
	}
	matches := durationRegex.FindStringSubmatch(duration)
	if matches == nil {
		if num, err := strconv.Atoi(duration); err == nil && num > 0 {
			return sql.NullInt64{Int64: int64(num), Valid: true}
		}
		return sql.NullInt64{Valid: false}
	}

	days := 0
	if matches[1] != "" {
		days, _ = strconv.Atoi(matches[1])
	}
	hours := 0
	if matches[2] != "" {
		hours, _ = strconv.Atoi(matches[2])
	}
	minutes := 0
	if matches[3] != "" {
		minutes, _ = strconv.Atoi(matches[3])
	}
	seconds := 0
	if matches[4] != "" {
		seconds, _ = strconv.Atoi(matches[4])
	}

	totalMinutes := (days * 24 * 60) + (hours * 60) + minutes
	if seconds >= 30 {
		totalMinutes++
	}

	if totalMinutes > 0 {
		return sql.NullInt64{Int64: int64(totalMinutes), Valid: true}
	}
	return sql.NullInt64{Valid: false}
}

func findAllJsonLd(n *nethtml.Node) []string {
	var scripts []string
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && strings.EqualFold(node.Data, "script") {
			for _, a := range node.Attr {
				if strings.EqualFold(a.Key, "type") && strings.EqualFold(a.Val, "application/ld+json") {
					var sb strings.Builder
					for c := node.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == nethtml.TextNode {
							sb.WriteString(c.Data)
						}
					}
					content := strings.TrimSpace(sb.String())
					if content != "" {
						scripts = append(scripts, content)
					}
					break
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return scripts
}

func findRecipeInJson(data json.RawMessage, schema *SchemaRecipe) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return false
	}

	if data[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(data, &list); err == nil {
			for _, item := range list {
				if findRecipeInJson(item, schema) {
					return true
				}
			}
		}
		return false
	}

	if data[0] == '{' {
		var graphObj struct {
			Graph []json.RawMessage `json:"@graph"`
		}
		if err := json.Unmarshal(data, &graphObj); err == nil && len(graphObj.Graph) > 0 {
			for _, item := range graphObj.Graph {
				if findRecipeInJson(item, schema) {
					return true
				}
			}
		}

		if checkItemForRecipe(data, schema) {
			return true
		}
	}

	return false
}

func checkItemForRecipe(item json.RawMessage, schema *SchemaRecipe) bool {
	var typeCheck struct {
		Type any `json:"@type"`
	}
	if err := json.Unmarshal(item, &typeCheck); err != nil {
		return false
	}

	isRecipe := false
	switch val := typeCheck.Type.(type) {
	case string:
		if strings.Contains(strings.ToLower(val), "recipe") {
			isRecipe = true
		}
	case []interface{}:
		for _, t := range val {
			if str, ok := t.(string); ok && strings.Contains(strings.ToLower(str), "recipe") {
				isRecipe = true
				break
			}
		}
	}

	if isRecipe {
		if err := json.Unmarshal(item, schema); err == nil {
			name := extractString(schema.Name)
			ings := extractIngredientStrings(schema.RecipeIngredient)
			if name != "" || len(ings) > 0 {
				return true
			}
		}
	}
	return false
}

func buildRecipeFromSchema(schema SchemaRecipe, url string) models.Recipe {
	var newRecipe models.Recipe
	newRecipe.Title = cleanText(extractString(schema.Name))
	newRecipe.Description = cleanText(extractString(schema.Description))
	newRecipe.Source = sql.NullString{String: url, Valid: true}

	prepStr := extractString(schema.PrepTime)
	cookStr := extractString(schema.CookTime)
	newRecipe.PrepTimeMinutes = parseISO8601Duration(prepStr)
	newRecipe.CookTimeMinutes = parseISO8601Duration(cookStr)

	if !newRecipe.PrepTimeMinutes.Valid && !newRecipe.CookTimeMinutes.Valid {
		totalStr := extractString(schema.TotalTime)
		if totalDuration := parseISO8601Duration(totalStr); totalDuration.Valid {
			newRecipe.CookTimeMinutes = totalDuration
		}
	}

	yieldStr := cleanText(extractYieldString(schema.RecipeYield))
	if yieldStr != "" {
		newRecipe.Servings = sql.NullString{String: yieldStr, Valid: true}
	}

	rawIngredients := extractIngredientStrings(schema.RecipeIngredient)
	newRecipe.Ingredients = make(models.IngredientsList, 0, len(rawIngredients))
	for i, raw := range rawIngredients {
		ing := parseIngredientString(raw, i)
		newRecipe.Ingredients = append(newRecipe.Ingredients, ing)
	}

	newRecipe.Instructions = extractInstructions(schema.RecipeInstructions)

	return newRecipe
}

type requestProfile struct {
	name    string
	headers map[string]string
}

var fallbackProfiles = []requestProfile{
	{
		name: "Browser",
		headers: map[string]string{
			"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:125.0) Gecko/20100101 Firefox/125.0",
			"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Accept-Language":           "en-US,en;q=0.5",
			"Upgrade-Insecure-Requests": "1",
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-User":            "?1",
		},
	},
	{
		name: "FacebookBot",
		headers: map[string]string{
			"User-Agent": "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)",
			"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		},
	},
	{
		name: "Slackbot",
		headers: map[string]string{
			"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)",
			"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		},
	},
	{
		name: "Twitterbot",
		headers: map[string]string{
			"User-Agent": "Twitterbot/1.0",
			"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		},
	},
}

func fetchWithFallback(ctx context.Context, targetURL string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) > 0 {
				for k, v := range via[0].Header {
					req.Header[k] = v
				}
			}
			return nil
		},
	}

	var lastResp *http.Response
	var lastErr error

	for _, p := range fallbackProfiles {
		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range p.headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("fetchWithFallback profile %s failed for %s: %v", p.name, targetURL, err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			log.Printf("fetchWithFallback succeeded with profile %s for %s", p.name, targetURL)
			return resp, nil
		}

		log.Printf("fetchWithFallback profile %s got HTTP %d for %s", p.name, resp.StatusCode, targetURL)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastResp = resp
	}

	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

// ImportRecipeFromURL fetches a web page and extracts recipe JSON-LD structured data.
func (s *Store) ImportRecipeFromURL(c echo.Context) error {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request. 'url' is required."})
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" || (!strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://")) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "A valid URL (starting with http:// or https://) is required."})
	}

	log.Printf("ImportRecipeFromURL called for URL: %s", req.URL)

	resp, err := fetchWithFallback(c.Request().Context(), req.URL)
	if err != nil {
		log.Printf("Error fetching URL %s: %v", req.URL, err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to fetch the URL: " + err.Error()})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error fetching URL %s: received HTTP status %d", req.URL, resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Could not access that URL (HTTP 403 Forbidden): this site blocks automated scrapers."})
		}
		if resp.StatusCode == http.StatusNotFound {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Could not access that URL (HTTP 404 Not Found): please verify the URL."})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Could not access that URL (HTTP %d).", resp.StatusCode)})
	}

	doc, err := nethtml.Parse(resp.Body)
	if err != nil {
		log.Printf("Error parsing HTML for %s: %v", req.URL, err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to parse page HTML."})
	}

	scripts := findAllJsonLd(doc)
	if len(scripts) == 0 {
		log.Printf("No JSON-LD scripts found for URL: %s", req.URL)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Could not find any recipe data on that page."})
	}

	var schema SchemaRecipe
	var found bool
	for _, script := range scripts {
		if findRecipeInJson(json.RawMessage(script), &schema) {
			found = true
			break
		}
	}

	if !found {
		log.Printf("Found %d JSON-LD script(s) on %s, but none contained a 'Recipe' schema", len(scripts), req.URL)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Found structured data, but could not find a 'Recipe' object."})
	}

	newRecipe := buildRecipeFromSchema(schema, req.URL)
	log.Printf("Successfully imported recipe '%s' from %s (%d ingredients, %d instructions)",
		newRecipe.Title, req.URL, len(newRecipe.Ingredients), len(newRecipe.Instructions))

	return c.JSON(http.StatusOK, newRecipe)
}
