package store

import (
	"log"
	"net/http"
	"strconv"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetMyCookbook fetches the full recipe details for all of the user's favorited recipes.
func (s *Store) GetMyCookbook(c echo.Context) error {
	userID := c.Get("userID").(string)

	query := `
		SELECT
			r.id, r.title, r.description, r.xp, r.tags, r.created_at,
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE((SELECT AVG(rating) FROM user_cooks_log WHERE recipe_id = r.id), 0) AS avg_rating,
			(SELECT COUNT(*) FROM user_cooks_log WHERE recipe_id = r.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		JOIN user_favorites f ON r.id = f.recipe_id
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE f.user_id = $1 AND (r.status = 'public' OR r.status = 'approved')
		ORDER BY f.created_at DESC
	`
	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying cookbook for user %s: %v", userID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch your cookbook"})
	}
	defer rows.Close()
	var recipes []models.Recipe
	for rows.Next() {
		var r models.Recipe
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
			&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
			&r.SubmittedByUsername,
			&r.Ingredients, &r.Instructions, &r.ImageURL, &r.Source,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug,
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings,
		); err != nil {
			log.Printf("Error scanning cookbook recipe: %v", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// AddFavorite adds a recipe to the current user's list of favorites.
func (s *Store) AddFavorite(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	_, err = s.DB.Exec(
		"INSERT INTO user_favorites (user_id, recipe_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		userID, recipeID,
	)
	if err != nil {
		log.Printf("Error adding favorite for user %s, recipe %d: %v", userID, recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not add to favorites"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe added to favorites"})
}

// RemoveFavorite removes a recipe from the user's list of favorites.
func (s *Store) RemoveFavorite(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	_, err = s.DB.Exec(
		"DELETE FROM user_favorites WHERE user_id = $1 AND recipe_id = $2",
		userID, recipeID,
	)
	if err != nil {
		log.Printf("Error removing favorite for user %s, recipe %d: %v", userID, recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not remove from favorites"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe removed from favorites"})
}

// GetFavoriteIDs fetches only the IDs of the recipes the user has favorited.
func (s *Store) GetFavoriteIDs(c echo.Context) error {
	userID := c.Get("userID").(string)

	query := "SELECT recipe_id FROM user_favorites WHERE user_id = $1"
	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error fetching favorite IDs for user %s: %v", userID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not fetch favorites"})
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("Error scanning favorite ID: %v", err)
			continue
		}
		ids = append(ids, id)
	}

	if ids == nil {
		ids = []int64{}
	}

	return c.JSON(http.StatusOK, ids)
}
