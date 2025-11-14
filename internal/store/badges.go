package store

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// getUserBadges fetches all badges earned by a specific user.
func (s *Store) getUserBadges(userID string) ([]models.Badge, error) {
	// MODIFIED: Added trigger_event and rule_config
	query := `
		SELECT 
			b.id, b.rule_key, b.name, b.description, b.icon_url, 
			b.badge_type, ub.earned_at, b.trigger_event, b.rule_config
		FROM badges b
		JOIN user_badges ub ON b.id = ub.badge_id
		WHERE ub.user_id = $1
		ORDER BY ub.earned_at DESC
	`
	rows, err := s.DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var badges []models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(
			&b.ID, &b.RuleKey, &b.Name, &b.Description,
			&b.IconURL, &b.BadgeType, &b.EarnedAt,
			&b.TriggerEvent, &b.RuleConfig, // <-- UPDATED
		); err != nil {
			return nil, err
		}
		badges = append(badges, b)
	}
	return badges, nil
}

// GetAllBadges fetches all badge definitions for the site admin portal.
func (s *Store) GetAllBadges(c echo.Context) error {
	// MODIFIED: Added trigger_event and rule_config
	query := `
		SELECT 
			id, rule_key, name, description, icon_url, 
			badge_type, start_date, end_date,
			trigger_event, rule_config
		FROM badges
		ORDER BY badge_type, name
	`
	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error fetching all badges: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch badges"})
	}
	defer rows.Close()

	var badges []models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(
			&b.ID, &b.RuleKey, &b.Name, &b.Description,
			&b.IconURL, &b.BadgeType, &b.StartDate, &b.EndDate,
			&b.TriggerEvent, &b.RuleConfig, // <-- UPDATED
		); err != nil {
			log.Printf("Error scanning badge definition row: %v\n", err)
			continue
		}
		badges = append(badges, b)
	}
	return c.JSON(http.StatusOK, badges)
}

// --- NEW: BadgeRequest struct for Create/Update ---
type BadgeRequest struct {
	RuleKey      string           `json:"rule_key"` // Now optional
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	IconURL      string           `json:"icon_url"`
	BadgeType    string           `json:"badge_type"`
	StartDate    *string          `json:"start_date"`
	EndDate      *string          `json:"end_date"`
	TriggerEvent string           `json:"trigger_event"`
	RuleConfig   *json.RawMessage `json:"rule_config"`
}

// CreateBadge allows a site admin to create a new badge definition.
func (s *Store) CreateBadge(c echo.Context) error {
	var req BadgeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// MODIFIED: Updated required fields
	if req.Name == "" || req.Description == "" || req.BadgeType == "" || req.TriggerEvent == "" || req.RuleConfig == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name, Description, BadgeType, TriggerEvent, and RuleConfig are required"})
	}

	var sqlRuleKey sql.NullString
	if req.RuleKey != "" {
		sqlRuleKey = sql.NullString{String: req.RuleKey, Valid: true}
	}
	var sqlIconURL sql.NullString
	if req.IconURL != "" {
		sqlIconURL = sql.NullString{String: req.IconURL, Valid: true}
	}
	var sqlStartDate sql.NullTime
	if req.StartDate != nil {
		t, err := time.Parse(time.RFC3339, *req.StartDate)
		if err == nil {
			sqlStartDate = sql.NullTime{Time: t, Valid: true}
		}
	}
	var sqlEndDate sql.NullTime
	if req.EndDate != nil {
		t, err := time.Parse(time.RFC3339, *req.EndDate)
		if err == nil {
			sqlEndDate = sql.NullTime{Time: t, Valid: true}
		}
	}

	// MODIFIED: Updated INSERT query
	query := `
		INSERT INTO badges (
			rule_key, name, description, icon_url, 
			badge_type, start_date, end_date,
			trigger_event, rule_config
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`
	var newID int64
	err := s.DB.QueryRow(
		query,
		sqlRuleKey, req.Name, req.Description,
		sqlIconURL, req.BadgeType, sqlStartDate, sqlEndDate,
		req.TriggerEvent, string(*req.RuleConfig), // <-- MODIFIED: Cast to string
	).Scan(&newID)

	if err != nil {
		log.Printf("Error creating new badge: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			// This could be on rule_key (if provided) or name (if you add a unique index)
			return c.JSON(http.StatusConflict, map[string]string{"error": "A badge with this RuleKey or Name already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create badge"})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{"message": "Badge created", "id": newID})
}

// UpdateBadge allows a site admin to update an existing badge definition.
func (s *Store) UpdateBadge(c echo.Context) error {
	badgeIDStr := c.Param("id")
	badgeID, err := strconv.ParseInt(badgeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid badge ID"})
	}

	var req BadgeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// MODIFIED: Updated required fields
	if req.Name == "" || req.Description == "" || req.BadgeType == "" || req.TriggerEvent == "" || req.RuleConfig == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name, Description, BadgeType, TriggerEvent, and RuleConfig are required"})
	}

	var sqlRuleKey sql.NullString
	if req.RuleKey != "" {
		sqlRuleKey = sql.NullString{String: req.RuleKey, Valid: true}
	}
	var sqlIconURL sql.NullString
	if req.IconURL != "" {
		sqlIconURL = sql.NullString{String: req.IconURL, Valid: true}
	}
	var sqlStartDate sql.NullTime
	if req.StartDate != nil {
		t, err := time.Parse(time.RFC3339, *req.StartDate)
		if err == nil {
			sqlStartDate = sql.NullTime{Time: t, Valid: true}
		}
	}
	var sqlEndDate sql.NullTime
	if req.EndDate != nil {
		t, err := time.Parse(time.RFC3339, *req.EndDate)
		if err == nil {
			sqlEndDate = sql.NullTime{Time: t, Valid: true}
		}
	}

	// MODIFIED: Updated UPDATE query
	query := `
		UPDATE badges
		SET 
			rule_key = $1, name = $2, description = $3, 
			icon_url = $4, badge_type = $5, start_date = $6, end_date = $7,
			trigger_event = $8, rule_config = $9
		WHERE id = $10
	`
	_, err = s.DB.Exec(
		query,
		sqlRuleKey, req.Name, req.Description,
		sqlIconURL, req.BadgeType, sqlStartDate, sqlEndDate,
		req.TriggerEvent, string(*req.RuleConfig), // <-- MODIFIED: Cast to string
		badgeID,
	)

	if err != nil {
		log.Printf("Error updating badge: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A badge with this RuleKey or Name already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update badge"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Badge updated"})
}
