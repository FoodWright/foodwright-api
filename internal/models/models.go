package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/lib/pq"
)

// --- JSONB Structs and Scanners ---

type Ingredient struct {
	Type        string `json:"type"`                   // "ingredient" or "header"
	Name        string `json:"name"`                   // For header: title, For ingredient: name
	QuantityStr string `json:"quantity_str,omitempty"` // "1 1/2", "0.5", "100"
	Unit        string `json:"unit,omitempty"`         // "cup", "g", "tsp"
	Order       int    `json:"order"`                  // Explicit ordering
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
	if err := json.Unmarshal(b, il); err != nil {
		return err
	}
	// Sort by Order field
	sort.Slice(*il, func(i, j int) bool {
		return (*il)[i].Order < (*il)[j].Order
	})
	return nil
}

type Instruction struct {
	Type  string `json:"type"` // "instruction" or "header"
	Step  string `json:"step"`
	Order int    `json:"order"` // Explicit ordering
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
	if err := json.Unmarshal(b, il); err != nil {
		return err
	}
	// Sort by Order field
	sort.Slice(*il, func(i, j int) bool {
		return (*il)[i].Order < (*il)[j].Order
	})
	return nil
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
	Source              sql.NullString   `json:"source"`
	AvgRating           float64          `json:"avg_rating"`
	CookCount           int              `json:"cook_count"`
	IsFeatured          bool             `json:"is_featured"`
	Slug                string           `json:"slug"`
	PrepTimeMinutes     sql.NullInt64    `json:"prep_time_minutes"`
	CookTimeMinutes     sql.NullInt64    `json:"cook_time_minutes"`
	Servings            sql.NullString   `json:"servings"`
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
	ImageURL  *string   `json:"image_url"`
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
	StartDate    sql.NullTime     `json:"start_date,omitempty"`
	EndDate      sql.NullTime     `json:"end_date,omitempty"`
	TriggerEvent string           `json:"trigger_event"`
	RuleConfig   *json.RawMessage `json:"rule_config"`
}

type UserProfile struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Rank           string    `json:"rank"`
	XP             int       `json:"xp"`
	Badges         []Badge   `json:"badges"`
	IsAdmin        bool      `json:"is_admin"`
	IsSiteAdmin    bool      `json:"is_site_admin"`
	UnitPreference string    `json:"unit_preference"` // "imperial" or "metric"
	FollowerCount  int       `json:"follower_count"`
	FollowingCount int       `json:"following_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PublicUserProfile struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Rank           string    `json:"rank"`
	XP             int       `json:"xp"`
	Badges         []Badge   `json:"badges"`
	FollowerCount  int       `json:"follower_count"`
	FollowingCount int       `json:"following_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type PublicCookLog struct {
	LogID       int64     `json:"log_id"`
	RecipeID    int64     `json:"recipe_id"`
	RecipeTitle string    `json:"recipe_title"`
	LoggedAt    time.Time `json:"logged_at"`
	Notes       *string   `json:"notes"`
	Rating      *int64    `json:"rating"`
	ImageURL    *string   `json:"image_url"`
}

// Post represents a feed item (cook_log, recipe_share, or quick_post)
type Post struct {
	ID          int64     `json:"id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	UserRank    string    `json:"user_rank"`
	PostType    string    `json:"post_type"` // 'cook_log', 'recipe_share', 'quick_post'
	RecipeID    *int64    `json:"recipe_id,omitempty"`
	RecipeTitle *string   `json:"recipe_title,omitempty"`
	RecipeSlug  *string   `json:"recipe_slug,omitempty"`
	RecipeImage *string   `json:"recipe_image,omitempty"`
	CookLogID   *int64    `json:"cook_log_id,omitempty"`
	Rating      *int64    `json:"rating,omitempty"`
	Notes       *string   `json:"notes,omitempty"`
	Content     *string   `json:"content,omitempty"`
	ImageURL    *string   `json:"image_url,omitempty"`
	ExternalURL *string   `json:"external_url,omitempty"`
	LikeCount   int       `json:"like_count"`
	RepostCount int       `json:"repost_count"`
	IsLiked     bool      `json:"is_liked"`
	IsReposted  bool      `json:"is_reposted"`
	RepostedBy  *string   `json:"reposted_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Follow represents a user follow relationship
type Follow struct {
	FollowerID string    `json:"follower_id"`
	FollowedID string    `json:"followed_id"`
	Username   string    `json:"username"`
	Rank       string    `json:"rank"`
	CreatedAt  time.Time `json:"created_at"`
}
