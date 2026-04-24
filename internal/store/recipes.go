package store

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/FoodWright/foodwright-api/internal/game"
	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

// GetRecipes fetches all public recipes with pagination and search.
func (s *Store) GetRecipes(c echo.Context) error {
	searchQuery := c.QueryParam("search")
	tagsQuery := c.QueryParam("tags")
	pageStr := c.QueryParam("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	const limit = 12
	offset := (page - 1) * limit

	baseQuery := `
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
	`
	countQuery := "SELECT COUNT(DISTINCT r.id) " + baseQuery
	recipesQuery := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE((SELECT AVG(rating) FROM user_cooks_log WHERE recipe_id = r.id), 0) AS avg_rating,
			(SELECT COUNT(*) FROM user_cooks_log WHERE recipe_id = r.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
	` + baseQuery

	// Only show public recipes in the global feed
	whereClauses := []string{"r.status = 'public'"}
	args := []interface{}{}
	argCount := 1

	if searchQuery != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.title ILIKE $%d", argCount))
		args = append(args, "%"+searchQuery+"%")
		argCount++
	}
	if tagsQuery != "" {
		tags := strings.Split(tagsQuery, ",")
		if len(tags) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("r.tags @> $%d::text[]", argCount))
			args = append(args, pq.Array(tags))
			argCount++
		}
	}

	fullWhere := " WHERE " + strings.Join(whereClauses, " AND ")
	countQuery += fullWhere
	recipesQuery += fullWhere

	recipesQuery += " ORDER BY r.created_at DESC"
	recipesQuery += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argCount, argCount+1)
	recipesArgs := append(args, limit, offset)

	var totalRecipes int
	err = s.DB.QueryRow(countQuery, args...).Scan(&totalRecipes)
	if err != nil {
		log.Printf("Error counting recipes: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to count recipes"})
	}
	totalPages := int(math.Ceil(float64(totalRecipes) / float64(limit)))
	if totalPages == 0 {
		totalPages = 1
	}

	rows, err := s.DB.Query(recipesQuery, recipesArgs...)
	if err != nil {
		log.Printf("Error querying recipes: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch recipes"})
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
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}

	return c.JSON(http.StatusOK, models.PaginatedRecipes{
		Recipes:     recipes,
		TotalPages:  totalPages,
		CurrentPage: page,
	})
}

// GetRecipeByID fetches a single recipe.
func (s *Store) GetRecipeByID(c echo.Context) error {
	idStr := c.Param("id")
	idParts := strings.Split(idStr, "-")
	id, err := strconv.ParseInt(idParts[0], 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	currentUserID, _ := c.Get("userID").(string)

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
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.id = $1
	`

	var r models.Recipe
	if err := s.DB.QueryRow(query, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
		&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
		&r.SubmittedByUsername,
		&r.Ingredients, &r.Instructions, &r.ImageURL, &r.Source,
		&r.AvgRating, &r.CookCount,
		&r.IsFeatured, &r.Slug,
		&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings,
	); err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch recipe"})
	}

	// Owner or Public check
	if r.Status == "private" && r.SubmittedByUserID.String != currentUserID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "This is a private recipe."})
	}

	return c.JSON(http.StatusOK, r)
}

// SubmitRecipe handles creating a public recipe immediately.
func (s *Store) SubmitRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)

	type SubmitRecipeRequest struct {
		Title           string                  `json:"title"`
		Description     string                  `json:"description"`
		Tags            []string                `json:"tags"`
		Ingredients     models.IngredientsList  `json:"ingredients"`
		Instructions    models.InstructionsList `json:"instructions"`
		ImageURL        string                  `json:"image_url"`
		Source          string                  `json:"source"`
		PrepTimeMinutes *int64                  `json:"prep_time_minutes"`
		CookTimeMinutes *int64                  `json:"cook_time_minutes"`
		Servings        string                  `json:"servings"`
	}
	var req SubmitRecipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	slug := game.Slugify(req.Title)
	var newRecipeID int64
	query := `
		INSERT INTO recipes (title, description, xp, tags, ingredients, instructions, status, submitted_by_user_id, image_url, source, slug, prep_time_minutes, cook_time_minutes, servings)
		VALUES ($1, $2, 10, $3, $4, $5, 'public', $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`
	err = tx.QueryRow(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions, userID, req.ImageURL, req.Source, slug, req.PrepTimeMinutes, req.CookTimeMinutes, req.Servings,
	).Scan(&newRecipeID)

	if err != nil {
		log.Printf("Error inserting recipe: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save recipe"})
	}

	// Post to feed
	_, _ = tx.Exec("INSERT INTO posts (user_id, post_type, recipe_id) VALUES ($1, 'recipe_share', $2)", userID, newRecipeID)

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Transaction error"})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{"id": newRecipeID, "message": "Recipe shared!"})
}

// GetMyRecipes fetches all recipes created by the authenticated user.
func (s *Store) GetMyRecipes(c echo.Context) error {
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
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.submitted_by_user_id = $1
		ORDER BY r.created_at DESC
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying my recipes: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch your recipes"})
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
			continue
		}
		recipes = append(recipes, r)
	}

	if recipes == nil {
		recipes = []models.Recipe{}
	}

	return c.JSON(http.StatusOK, recipes)
}

// CreatePrivateRecipe saves a recipe as a draft.
func (s *Store) CreatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)

	type PrivateRecipeRequest struct {
		Title           string                  `json:"title"`
		Description     string                  `json:"description"`
		Tags            []string                `json:"tags"`
		Ingredients     models.IngredientsList  `json:"ingredients"`
		Instructions    models.InstructionsList `json:"instructions"`
		ImageURL        string                  `json:"image_url"`
		Source          string                  `json:"source"`
		PrepTimeMinutes *int64                  `json:"prep_time_minutes"`
		CookTimeMinutes *int64                  `json:"cook_time_minutes"`
		Servings        string                  `json:"servings"`
	}
	var req PrivateRecipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	slug := game.Slugify(req.Title)
	var newRecipeID int64
	query := `
		INSERT INTO recipes (title, description, tags, ingredients, instructions, status, submitted_by_user_id, image_url, source, slug, prep_time_minutes, cook_time_minutes, servings)
		VALUES ($1, $2, $3, $4, $5, 'private', $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions, userID, req.ImageURL, req.Source, slug, req.PrepTimeMinutes, req.CookTimeMinutes, req.Servings,
	).Scan(&newRecipeID)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save draft"})
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{"id": newRecipeID, "message": "Draft saved!"})
}

// UpdatePrivateRecipe handles updates to a recipe.
func (s *Store) UpdatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, _ := strconv.ParseInt(recipeIDStr, 10, 64)

	type UpdateRequest struct {
		Title           string                  `json:"title"`
		Description     string                  `json:"description"`
		Tags            []string                `json:"tags"`
		Ingredients     models.IngredientsList  `json:"ingredients"`
		Instructions    models.InstructionsList `json:"instructions"`
		ImageURL        string                  `json:"image_url"`
		Source          string                  `json:"source"`
		PrepTimeMinutes *int64                  `json:"prep_time_minutes"`
		CookTimeMinutes *int64                  `json:"cook_time_minutes"`
		Servings        string                  `json:"servings"`
	}
	var req UpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	slug := game.Slugify(req.Title)
	query := `
		UPDATE recipes
		SET title = $1, description = $2, tags = $3, ingredients = $4, instructions = $5, image_url = $6, source = $7, slug = $8, prep_time_minutes = $9, cook_time_minutes = $10, servings = $11, updated_at = NOW()
		WHERE id = $12 AND submitted_by_user_id = $13
	`
	_, err := s.DB.Exec(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions, req.ImageURL, req.Source, slug, req.PrepTimeMinutes, req.CookTimeMinutes, req.Servings,
		recipeID, userID,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update recipe"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe updated!"})
}

// DeletePrivateRecipe handles deletion.
func (s *Store) DeletePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, _ := strconv.ParseInt(recipeIDStr, 10, 64)

	_, err := s.DB.Exec("DELETE FROM recipes WHERE id = $1 AND submitted_by_user_id = $2", recipeID, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete recipe"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe deleted"})
}

// SubmitPrivateRecipe handles making a draft public and sharing it to feed.
func (s *Store) SubmitPrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, _ := strconv.ParseInt(recipeIDStr, 10, 64)

	tx, err := s.DB.Begin()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	// Set public
	_, err = tx.Exec("UPDATE recipes SET status = 'public', updated_at = NOW() WHERE id = $1 AND submitted_by_user_id = $2", recipeID, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to publish"})
	}

	// Create feed post
	_, err = tx.Exec("INSERT INTO posts (user_id, post_type, recipe_id) VALUES ($1, 'recipe_share', $2)", userID, recipeID)
	if err != nil {
		log.Printf("Error creating feed post: %v\n", err)
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Commit error"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe shared to feed!"})
}

// GetFeaturedRecipes fetches recipes marked as featured.
func (s *Store) GetFeaturedRecipes(c echo.Context) error {
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
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.is_featured = TRUE AND r.status = 'public'
		ORDER BY r.updated_at DESC
	`
	rows, err := s.DB.Query(query)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch featured recipes"})
	}
	defer rows.Close()

	var recipes []models.Recipe
	for rows.Next() {
		var r models.Recipe
		err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
			&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
			&r.SubmittedByUsername,
			&r.Ingredients, &r.Instructions, &r.ImageURL, &r.Source,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug,
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings,
		)
		if err != nil {
			continue
		}
		recipes = append(recipes, r)
	}

	return c.JSON(http.StatusOK, recipes)
}

func (s *Store) ImportRecipeFromURL(c echo.Context) error {
	type ImportRequest struct {
		URL string `json:"url"`
	}
	var req ImportRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	newRecipe := models.Recipe{
		Title:       "Imported Recipe",
		Description: "Imported from " + req.URL,
		Ingredients: models.IngredientsList{
			{Name: "Check source URL for details", QuantityStr: "1", Unit: "each"},
		},
		Instructions: models.InstructionsList{
			{Step: "Review the imported content and save."},
		},
		Source: sql.NullString{String: req.URL, Valid: true},
	}

	return c.JSON(http.StatusOK, newRecipe)
}
