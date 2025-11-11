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

// Ingredient defines the structure for a single ingredient
type Ingredient struct {
	Quantity string `json:"quantity"`
	Name     string `json:"name"`
}

// IngredientsList is a slice of Ingredient that implements sql.Scanner and driver.Valuer
type IngredientsList []Ingredient

// Value implements the driver.Valuer interface, converting our list to JSON
func (il IngredientsList) Value() (driver.Value, error) {
	if il == nil {
		il = []Ingredient{} // Ensure '[]' instead of 'null'
	}
	// --- FIX: Return string, not []byte ---
	data, err := json.Marshal(il)
	return string(data), err
}

// Scan implements the sql.Scanner interface, converting JSON from DB to our list
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

// Instruction defines the structure for a single step
type Instruction struct {
	Step string `json:"step"`
}

// InstructionsList is a slice of Instruction that implements sql.Scanner and driver.Valuer
type InstructionsList []Instruction

// Value implements the driver.Valuer interface
func (il InstructionsList) Value() (driver.Value, error) {
	if il == nil {
		il = []Instruction{} // Ensure '[]' instead of 'null'
	}
	// --- FIX: Return string, not []byte ---
	data, err := json.Marshal(il)
	return string(data), err
}

// Scan implements the sql.Scanner interface
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
	ImageURL            sql.NullString   `json:"image_url"` // <-- NEW FIELD
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

type UserProfile struct {
	ID        string         `json:"id"`
	Username  string         `json:"username"`
	Rank      string         `json:"rank"`
	XP        int            `json:"xp"`
	Badges    pq.StringArray `json:"badges"`
	IsAdmin   bool           `json:"is_admin"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type PublicUserProfile struct {
	ID        string         `json:"id"`
	Username  string         `json:"username"`
	Rank      string         `json:"rank"`
	XP        int            `json:"xp"`
	Badges    pq.StringArray `json:"badges"`
	CreatedAt time.Time      `json:"created_at"`
}

type PublicCookLog struct {
	LogID       int64     `json:"log_id"`
	RecipeID    int64     `json:"recipe_id"`
	RecipeTitle string    `json:"recipe_title"`
	LoggedAt    time.Time `json:"logged_at"`
	Notes       *string   `json:"notes"`
	Rating      *int64    `json:"rating"`
}
