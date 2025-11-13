package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
)

// --- JSONB Structs and Scanners ---

type Ingredient struct {
	Quantity string `json:"quantity"`
	Name     string `json:"name"`
}
type IngredientsList []Ingredient

func (il IngredientsList) Value() (driver.Value, error) {
	if il == nil {
		il = []Ingredient{}
	}
	data, err := json.Marshal(il)
	return string(data), err
}
func (il *IngredientsList) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	if string(b) == "null" {
		*il = []Ingredient{}
		return nil
	}
	return json.Unmarshal(b, il)
}

type Instruction struct {
	Step string `json:"step"`
}
type InstructionsList []Instruction

func (il InstructionsList) Value() (driver.Value, error) {
	if il == nil {
		il = []Instruction{}
	}
	data, err := json.Marshal(il)
	return string(data), err
}
func (il *InstructionsList) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	if string(b) == "null" {
		*il = []Instruction{}
		return nil
	}
	return json.Unmarshal(b, il)
}

// --- End JSONB Structs ---

// --- Model Structs ---

type Recipe struct {
	ID                  int64            `json:"id"`
	Title               string           `json:"title"`
	Description         string           `json:"description"`
	XP                  int              `json:"xp"`
	Tags                pq.StringArray   `json:"tags"`
	CreatedAt           time.Time        `json:"created_at"`
	Status              string           `json:"status"`
	SubmittedByUserID   sql.NullString   `json:"submitted_by_user_id"`
	SubmittedByUsername sql.NullString   `json:"submitted_by_username"`
	Ingredients         IngredientsList  `json:"ingredients"`
	Instructions        InstructionsList `json:"instructions"`
	ImageURL            sql.NullString   `json:"image_url"`
	AvgRating           float64          `json:"avg_rating"`
	CookCount           int              `json:"cook_count"`
	IsFeatured          bool             `json:"is_featured"`
	Slug                string           `json:"slug"`
}

type PaginatedRecipes struct {
	Recipes     []Recipe `json:"recipes"`
	TotalPages  int      `json:"total_pages"`
	CurrentPage int      `json:"current_page"`
}

type DBUserCookLog struct {
	ID        int64
	UserID    string
	Username  string
	Rating    sql.NullInt64
	Notes     sql.NullString
	CreatedAt time.Time
}

type CleanCookLog struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Rating    *int64    `json:"rating"`
	Notes     *string   `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

type RecipeComment struct {
	ID        int64     `json:"id"`
	RecipeID  int64     `json:"recipe_id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

type Badge struct {
	ID           int64            `json:"id"`
	RuleKey      sql.NullString   `json:"rule_key"` // Kept for reference, but now nullable
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	IconURL      sql.NullString   `json:"icon_url"`
	BadgeType    string           `json:"badge_type"`
	EarnedAt     sql.NullTime     `json:"earned_at,omitempty"`
	StartDate    sql.NullTime     `json:"start_date,omitempty"` // For admin UI
	EndDate      sql.NullTime     `json:"end_date,omitempty"`   // For admin UI
	TriggerEvent string           `json:"trigger_event"`        // NEW: 'on_cook', 'on_approval'
	RuleConfig   *json.RawMessage `json:"rule_config"`          // NEW: The JSON rule logic
}

type UserProfile struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Rank        string    `json:"rank"`
	XP          int       `json:"xp"`
	Badges      []Badge   `json:"badges"`
	IsAdmin     bool      `json:"is_admin"`
	IsSiteAdmin bool      `json:"is_site_admin"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PublicUserProfile struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Rank      string    `json:"rank"`
	XP        int       `json:"xp"`
	Badges    []Badge   `json:"badges"`
	CreatedAt time.Time `json:"created_at"`
}

// --- End Model Structs ---

type PublicCookLog struct {
	LogID       int64     `json:"log_id"`
	RecipeID    int64     `json:"recipe_id"`
	RecipeTitle string    `json:"recipe_title"`
	LoggedAt    time.Time `json:"logged_at"`
	Notes       *string   `json:"notes"`
	Rating      *int64    `json:"rating"`
}
