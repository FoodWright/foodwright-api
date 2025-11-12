package store

import (
	"log"
	"net/http"
	"strconv"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetMyFavoriteIDs fetches a list of recipe IDs that the current user has favorited.
func (s *Store) GetMyFavoriteIDs(c echo.Context) error {
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
	return c.JSON(http.StatusOK, ids)
}

// GetMyCookbook fetches the full recipe details for all of the user's favorited recipes.
func (s *Store) GetMyCookbook(c echo.Context) error {
	userID := c.Get("userID").(string)
	// --- MODIFIED: Added LEFT JOIN for logs and GROUP BY ---
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured,
			r.slug
		FROM recipes r
		JOIN user_favorites f ON r.id = f.recipe_id
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE f.user_id = $1 AND r.status = 'approved'
		GROUP BY r.id, u.username, f.created_at
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount, &r.IsFeatured, &r.Slug,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
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
	query := "INSERT INTO user_favorites (user_id, recipe_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	_, err = s.DB.Exec(query, userID, recipeID)
	if err != nil {
		log.Printf("Error adding favorite for user %s, recipe %d: %v", userID, recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not add favorite"})
	}
	return c.JSON(http.StatusCreated, map[string]string{"message": "Recipe added to cookbook"})
}

// RemoveFavorite removes a recipe from the current user's list of favorites.
func (s *Store) RemoveFavorite(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	query := "DELETE FROM user_favorites WHERE user_id = $1 AND recipe_id = $2"
	_, err = s.DB.Exec(query, userID, recipeID)
	if err != nil {
		log.Printf("Error removing favorite for user %s, recipe %d: %v", userID, recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not remove favorite"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe removed from cookbook"})
}
