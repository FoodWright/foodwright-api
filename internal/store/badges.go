package store

import (
	"database/sql"
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
	query := `
		SELECT b.id, b.rule_key, b.name, b.description, b.icon_url, b.badge_type, ub.earned_at
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
		); err != nil {
			return nil, err
		}
		badges = append(badges, b)
	}
	return badges, nil
}

// GetAllBadges fetches all badge definitions for the site admin portal.
func (s *Store) GetAllBadges(c echo.Context) error {
	query := `
		SELECT id, rule_key, name, description, icon_url, badge_type, start_date, end_date
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
		var startDate sql.NullTime
		var endDate sql.NullTime

		if err := rows.Scan(
			&b.ID, &b.RuleKey, &b.Name, &b.Description,
			&b.IconURL, &b.BadgeType, &startDate, &endDate,
		); err != nil {
			log.Printf("Error scanning badge definition row: %v\n", err)
			continue
		}
		badges = append(badges, b)
	}
	return c.JSON(http.StatusOK, badges)
}

// CreateBadge allows a site admin to create a new badge definition.
func (s *Store) CreateBadge(c echo.Context) error {
	type BadgeRequest struct {
		RuleKey     string  `json:"rule_key"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		IconURL     string  `json:"icon_url"`
		BadgeType   string  `json:"badge_type"`
		StartDate   *string `json:"start_date"`
		EndDate     *string `json:"end_date"`
	}
	var req BadgeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	if req.RuleKey == "" || req.Name == "" || req.Description == "" || req.BadgeType == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "RuleKey, Name, Description, and BadgeType are required"})
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

	query := `
		INSERT INTO badges (rule_key, name, description, icon_url, badge_type, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`
	var newID int64
	err := s.DB.QueryRow(
		query,
		req.RuleKey, req.Name, req.Description,
		sqlIconURL, req.BadgeType, sqlStartDate, sqlEndDate,
	).Scan(&newID)

	if err != nil {
		log.Printf("Error creating new badge: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A badge with this RuleKey already exists."})
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

	type BadgeRequest struct {
		RuleKey     string  `json:"rule_key"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		IconURL     string  `json:"icon_url"`
		BadgeType   string  `json:"badge_type"`
		StartDate   *string `json:"start_date"`
		EndDate     *string `json:"end_date"`
	}
	var req BadgeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	if req.RuleKey == "" || req.Name == "" || req.Description == "" || req.BadgeType == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "RuleKey, Name, Description, and BadgeType are required"})
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

	query := `
		UPDATE badges
		SET rule_key = $1, name = $2, description = $3, 
		    icon_url = $4, badge_type = $5, start_date = $6, end_date = $7
		WHERE id = $8
	`
	_, err = s.DB.Exec(
		query,
		req.RuleKey, req.Name, req.Description,
		sqlIconURL, req.BadgeType, sqlStartDate, sqlEndDate,
		badgeID,
	)

	if err != nil {
		log.Printf("Error updating badge: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A badge with this RuleKey already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update badge"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Badge updated"})
}

// ToggleRecipeFeature allows a site admin to mark/unmark a recipe as featured.
func (s *Store) ToggleRecipeFeature(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	var isFeatured bool
	query := "UPDATE recipes SET is_featured = NOT is_featured WHERE id = $1 RETURNING is_featured"
	err = s.DB.QueryRow(query, recipeID).Scan(&isFeatured)
	if err != nil {
		log.Printf("Error toggling featured status for recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update recipe"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":     "Featured status updated",
		"id":          recipeID,
		"is_featured": isFeatured,
	})
}
