package store

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"github.com/FoodWright/foodwright-api/internal/game"
	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetCookLogsForRecipe fetches the 50 most recent cook logs for a specific recipe.
func (s *Store) GetCookLogsForRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	query := `
		SELECT l.id, l.user_id, u.username, l.rating, l.notes, l.created_at
		FROM user_cooks_log l
		JOIN users u ON l.user_id = u.id
		WHERE l.recipe_id = $1
		ORDER BY l.created_at DESC
		LIMIT 50
	`
	rows, err := s.DB.Query(query, recipeID)
	if err != nil {
		log.Printf("Error querying cook logs: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch cook logs"})
	}
	defer rows.Close()

	var dbLogs []models.DBUserCookLog
	for rows.Next() {
		var l models.DBUserCookLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Rating, &l.Notes, &l.CreatedAt); err != nil {
			log.Printf("Error scanning cook log row: %v\n", err)
			continue
		}
		dbLogs = append(dbLogs, l)
	}

	cleanLogs := make([]models.CleanCookLog, len(dbLogs))
	for i, dbLog := range dbLogs {
		cleanLogs[i] = models.CleanCookLog{
			ID:        dbLog.ID,
				UserID:    dbLog.UserID,
				Username:  dbLog.Username,
				CreatedAt: dbLog.CreatedAt,
		}
		if dbLog.Rating.Valid {
			cleanLogs[i].Rating = &dbLog.Rating.Int64
		}
		if dbLog.Notes.Valid {
			cleanLogs[i].Notes = &dbLog.Notes.String
		}
	}
	return c.JSON(http.StatusOK, cleanLogs)
}

// GetPublicCookLogs fetches the 10 most recent, public cook logs for a specific user.
func (s *Store) GetPublicCookLogs(c echo.Context) error {
	userID := c.Param("id")
	log.Printf("Fetching PUBLIC cook logs for user: %s", userID)
	query := `
		SELECT l.id, l.recipe_id, r.title, l.created_at, l.notes, l.rating
		FROM user_cooks_log l
		JOIN recipes r ON l.recipe_id = r.id
		WHERE l.user_id = $1 AND r.status = 'approved'
		ORDER BY l.created_at DESC
		LIMIT 10
	`
	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying public cook logs: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch logs"})
	}
	defer rows.Close()
	var logs []models.PublicCookLog
	for rows.Next() {
		var l models.PublicCookLog
		var sqlNotes sql.NullString
		var sqlRating sql.NullInt64
		if err := rows.Scan(&l.LogID, &l.RecipeID, &l.RecipeTitle, &l.LoggedAt, &sqlNotes, &sqlRating); err != nil {
			log.Printf("Error scanning public cook log row: %v\n", err)
			continue
		}
		if sqlNotes.Valid {
			l.Notes = &sqlNotes.String
		}
		if sqlRating.Valid {
			l.Rating = &sqlRating.Int64
		}
		logs = append(logs, l)
	}
	return c.JSON(http.StatusOK, logs)
}

// LogCook allows a user to log that they have cooked a recipe, awarding XP and checking for new badges.
func (s *Store) LogCook(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	type LogCookRequest struct {
		Notes  string `json:"notes"`
		Rating *int   `json:"rating"`
	}
	var req LogCookRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	var sqlNotes sql.NullString
	if req.Notes != "" {
		sqlNotes = sql.NullString{String: req.Notes, Valid: true}
	}
	var sqlRating sql.NullInt64
	if req.Rating != nil && *req.Rating > 0 {
		sqlRating = sql.NullInt64{Int64: int64(*req.Rating), Valid: true}
	}

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	var recipeXP int
	err = tx.QueryRow("SELECT xp FROM recipes WHERE id = $1", recipeID).Scan(&recipeXP)
	if err != nil {
		log.Printf("Error getting recipe XP: %v\n", err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}

	var currentXP int
	var currentRank string
	err = tx.QueryRow("SELECT xp, rank FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&currentXP, &currentRank)
	if err != nil {
		log.Printf("Error getting user profile for update: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to get user profile"})
	}

	newlyAwardedBadges, err := game.CheckAndAwardBadges(tx, userID, recipeID)
	if err != nil {
		log.Printf("Error in Badge Engine: %v. Continuing with log...\n", err)
		newlyAwardedBadges = []string{}
	}

	_, err = tx.Exec(
		"INSERT INTO user_cooks_log (user_id, recipe_id, notes, rating) VALUES ($1, $2, $3, $4)",
		userID, recipeID, sqlNotes, sqlRating,
	)
	if err != nil {
		log.Printf("Error inserting cook log: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to log cook record"})
	}

	newTotalXP := currentXP + recipeXP
	newRank := game.CalculateRank(newTotalXP)
	didRankUp := newRank != currentRank

	updateQuery := `
		UPDATE users
		SET xp = $1, rank = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err = tx.Exec(updateQuery, newTotalXP, newRank, userID)
	if err != nil {
		log.Printf("Error updating user profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update user profile"})
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save cook log"})
	}

	log.Printf("User %s logged cook. XP: %d -> %d. Rank Up: %t. Badges Awarded: %v\n", userID, currentXP, newTotalXP, didRankUp, newlyAwardedBadges)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":            "Cook logged successfully!",
		"xp_granted":         recipeXP,
		"new_total_xp":       newTotalXP,
		"rank_up":            didRankUp,
		"new_rank":           newRank,
		"new_badges_awarded": newlyAwardedBadges,
	})
}