package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/FoodWright/foodwright-api/internal/game"
	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"golang.org/x/net/html"
)

// GetRecipes handles fetching a paginated, searchable, and tag-filterable list of approved recipes.
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
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
	`
	countBaseQuery := `
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
	`
	countQuery := "SELECT COUNT(DISTINCT r.id) " + countBaseQuery
	recipesQuery := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
	` + baseQuery

	// MODIFIED: Filter out featured recipes from the main paginated list
	whereClauses := []string{"r.status = 'approved'", "r.is_featured = FALSE"}
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

	recipesQuery += " GROUP BY r.id, u.username"
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
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}

	response := models.PaginatedRecipes{
		Recipes:     recipes,
		TotalPages:  totalPages,
		CurrentPage: page,
	}
	return c.JSON(http.StatusOK, response)
}

// GetRecipeByID fetches a single recipe by its ID, handling private recipe access control.
func (s *Store) GetRecipeByID(c echo.Context) error {
	idStr := c.Param("id") // This will be "9-simple-sourdough"

	// --- PARSE THE ID ---
	idParts := strings.SplitN(idStr, "-", 2)
	id, err := strconv.ParseInt(idParts[0], 10, 64) // Parse just the "9"
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	// ---
	currentUserID, _ := c.Get("userID").(string)

	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.id = $1
		GROUP BY r.id, u.username
	`

	var r models.Recipe
	if err := s.DB.QueryRow(query, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
		&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
		&r.SubmittedByUsername,
		&r.Ingredients, &r.Instructions, &r.ImageURL, &r.Source,
		&r.AvgRating, &r.CookCount,
		&r.IsFeatured, &r.Slug,
		&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
	); err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
		}
		log.Printf("Error scanning recipe from query: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}

	if r.Status == "private" && r.SubmittedByUserID.String != currentUserID {
		log.Printf("Access denied for recipe %d: User %s is not owner", r.ID, currentUserID)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}

	return c.JSON(http.StatusOK, r)
}

// SubmitRecipe
func (s *Store) SubmitRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	type SubmitRecipeRequest struct {
		Title        string                  `json:"title"`
		Description  string                  `json:"description"`
		XP           int                     `json:"xp"`
		Tags         []string                `json:"tags"`
		Ingredients  models.IngredientsList  `json:"ingredients"`
		Instructions models.InstructionsList `json:"instructions"`
		ImageURL     string                  `json:"image_url"`
		// Note: Source, times, and servings are not included here,
		// as this is for direct submission, not the private edit form.
	}
	var req SubmitRecipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	if req.Title == "" || req.Description == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Title and Description are required"})
	}
	if req.XP < 10 || req.XP > 100 {
		req.XP = 10
	}
	if req.Ingredients == nil {
		req.Ingredients = []models.Ingredient{}
	}
	if req.Instructions == nil {
		req.Instructions = []models.Instruction{}
	}
	var sqlImageURL sql.NullString
	if req.ImageURL != "" {
		sqlImageURL = sql.NullString{String: req.ImageURL, Valid: true}
	}

	slug := game.Slugify(req.Title) // Create slug

	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, created_at, updated_at, slug
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10)
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, req.XP, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		"pending", userID,
		sqlImageURL, slug,
	).Scan(&newRecipeID)
	if err != nil {
		log.Printf("Error inserting submitted recipe: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}
	log.Printf("User %s submitted new recipe (ID: %d)", userID, newRecipeID)
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  "Recipe submitted for review!",
		"recipeId": newRecipeID,
	})
}

// GetMySubmissions fetches all recipes submitted by the currently authenticated user.
func (s *Store) GetMySubmissions(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.submitted_by_user_id = $1
		GROUP BY r.id, u.username
		ORDER BY r.created_at DESC
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying my submissions: %v\n", err)
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
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// GetPendingRecipes fetches all recipes with a 'pending' status for admin review.
func (s *Store) GetPendingRecipes(c echo.Context) error {
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.status = 'pending'
		GROUP BY r.id, u.username
		ORDER BY r.created_at ASC
	`

	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error querying pending recipes: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch pending recipes"})
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
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// ApproveRecipe allows an admin to approve a recipe and set its XP
func (s *Store) ApproveRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	// --- NEW: Bind the request to get the XP value ---
	type ApproveRequest struct {
		XP int `json:"xp"`
	}
	var req ApproveRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	if req.XP < 10 || req.XP > 100 {
		log.Printf("Admin specified invalid XP %d for recipe %d, defaulting to 10.", req.XP, recipeID)
		req.XP = 10
	}
	// ---

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	var submitterID sql.NullString
	// --- MODIFIED: Update XP along with status ---
	err = tx.QueryRow(
		"UPDATE recipes SET status = 'approved', xp = $1, updated_at = NOW() WHERE id = $2 RETURNING submitted_by_user_id",
		req.XP, recipeID,
	).Scan(&submitterID)
	// ---
	if err != nil {
		log.Printf("Error approving recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to approve recipe"})
	}

	if submitterID.Valid {
		submitterUserID := submitterID.String
		const approvalXP = 250 // Base XP for getting *any* recipe approved

		// --- MODIFIED: Call new dynamic badge engine ---
		newlyAwardedBadges, err := game.CheckAndAwardBadges(tx, submitterUserID, recipeID, "on_approval")
		if err != nil {
			log.Printf("Error checking for 'on_approval' badges for user %s: %v", submitterUserID, err)
		} else if len(newlyAwardedBadges) > 0 {
			log.Printf("User %s awarded new badges: %v", submitterUserID, newlyAwardedBadges)
		}
		// ---

		_, err = tx.Exec(
			"UPDATE users SET xp = xp + $1, updated_at = NOW() WHERE id = $2",
			approvalXP, submitterUserID,
		)
		if err != nil {
			log.Printf("Failed to award approval XP to user %s: %v", submitterUserID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing recipe approval: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save approval"})
	}
	log.Printf("Recipe %d approved with %d XP by admin %s", recipeID, req.XP, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe approved and XP awarded!"})
}

// RejectRecipe (no changes)
func (s *Store) RejectRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	_, err = s.DB.Exec("UPDATE recipes SET status = 'rejected', updated_at = NOW() WHERE id = $1", recipeID)
	if err != nil {
		log.Printf("Error rejecting recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reject recipe"})
	}
	log.Printf("Recipe %d rejected by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe rejected."})
}

// PrivateRecipeRequest is the struct for creating/updating private recipes
type PrivateRecipeRequest struct {
	Title           string                  `json:"title"`
	Description     string                  `json:"description"`
	Tags            []string                `json:"tags"`
	Ingredients     models.IngredientsList  `json:"ingredients"`
	Instructions    models.InstructionsList `json:"instructions"`
	ImageURL        string                  `json:"image_url"`
	Source          string                  `json:"source"`
	PrepTimeMinutes *int                    `json:"prep_time_minutes"` // Use pointer for nullable
	CookTimeMinutes *int                    `json:"cook_time_minutes"` // Use pointer for nullable
	Servings        string                  `json:"servings"`
}

// CreatePrivateRecipe
func (s *Store) CreatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)

	var req PrivateRecipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	if req.Title == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Title is required"})
	}
	if req.Ingredients == nil {
		req.Ingredients = []models.Ingredient{}
	}
	if req.Instructions == nil {
		req.Instructions = []models.Instruction{}
	}
	var sqlImageURL sql.NullString
	if req.ImageURL != "" {
		sqlImageURL = sql.NullString{String: req.ImageURL, Valid: true}
	}
	var sqlSource sql.NullString
	if req.Source != "" {
		sqlSource = sql.NullString{String: req.Source, Valid: true}
	}
	// --- Handle new fields ---
	var sqlPrepTime sql.NullInt64
	if req.PrepTimeMinutes != nil {
		sqlPrepTime = sql.NullInt64{Int64: int64(*req.PrepTimeMinutes), Valid: true}
	}
	var sqlCookTime sql.NullInt64
	if req.CookTimeMinutes != nil {
		sqlCookTime = sql.NullInt64{Int64: int64(*req.CookTimeMinutes), Valid: true}
	}
	var sqlServings sql.NullString
	if req.Servings != "" {
		sqlServings = sql.NullString{String: req.Servings, Valid: true}
	}
	// ---

	slug := game.Slugify(req.Title) // Create slug

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, source, created_at, updated_at, slug,
			prep_time_minutes, cook_time_minutes, servings
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW(), $11, $12, $13, $14)
		RETURNING id
	`
	var newRecipeID int64
	err = tx.QueryRow(
		query,
		req.Title, req.Description, 0, pq.Array(req.Tags), // XP is 0
		req.Ingredients, req.Instructions,
		"private", userID, // Status is 'private'
		sqlImageURL, sqlSource, slug,
		sqlPrepTime, sqlCookTime, sqlServings, // Added new fields
	).Scan(&newRecipeID)
	if err != nil {
		log.Printf("Error inserting private recipe: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}

	// --- NEW: Call badge engine ---
	newlyAwardedBadges, err := game.CheckAndAwardBadges(tx, userID, newRecipeID, "on_private_save")
	if err != nil {
		// Log the error but don't fail the transaction
		log.Printf("Error checking for 'on_private_save' badges for user %s: %v", userID, err)
		newlyAwardedBadges = []string{} // Ensure it's an empty list
	}
	// ---

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing private recipe: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save recipe"})
	}

	log.Printf("User %s created new private recipe (ID: %d)", userID, newRecipeID)
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":            "Recipe saved to your private cookbook!",
		"recipeId":           newRecipeID,
		"new_badges_awarded": newlyAwardedBadges,
	})
}

// GetMyPrivateRecipes fetches all recipes owned by the user with a 'private' status.
func (s *Store) GetMyPrivateRecipes(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.submitted_by_user_id = $1 AND r.status = 'private'
		GROUP BY r.id, u.username
		ORDER BY r.created_at DESC
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying my private recipes: %v\n", err)
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
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// UpdatePrivateRecipe
func (s *Store) UpdatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	var req PrivateRecipeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	if req.Title == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Title is required"})
	}
	if req.Ingredients == nil {
		req.Ingredients = []models.Ingredient{}
	}
	if req.Instructions == nil {
		req.Instructions = []models.Instruction{}
	}
	var sqlImageURL sql.NullString
	if req.ImageURL != "" {
		sqlImageURL = sql.NullString{String: req.ImageURL, Valid: true}
	}
	var sqlSource sql.NullString
	if req.Source != "" {
		sqlSource = sql.NullString{String: req.Source, Valid: true}
	}
	// --- Handle new fields ---
	var sqlPrepTime sql.NullInt64
	if req.PrepTimeMinutes != nil {
		sqlPrepTime = sql.NullInt64{Int64: int64(*req.PrepTimeMinutes), Valid: true}
	}
	var sqlCookTime sql.NullInt64
	if req.CookTimeMinutes != nil {
		sqlCookTime = sql.NullInt64{Int64: int64(*req.CookTimeMinutes), Valid: true}
	}
	var sqlServings sql.NullString
	if req.Servings != "" {
		sqlServings = sql.NullString{String: req.Servings, Valid: true}
	}
	// ---

	slug := game.Slugify(req.Title) // Create slug

	query := `
		UPDATE recipes
		SET 
			title = $1, description = $2, tags = $3,
			ingredients = $4, instructions = $5, updated_at = NOW(),
			image_url = $6, source = $7, slug = $8,
			prep_time_minutes = $9, cook_time_minutes = $10, servings = $11
		WHERE id = $12 AND submitted_by_user_id = $13 AND (status = 'private' OR status = 'rejected')
	`
	res, err := s.DB.Exec(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		sqlImageURL, sqlSource, slug,
		sqlPrepTime, sqlCookTime, sqlServings, // Added new fields
		recipeID, userID,
	)
	if err != nil {
		log.Printf("Error updating private recipe: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found or you do not have permission to edit it."})
	}

	log.Printf("User %s updated private recipe (ID: %d)", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Recipe updated successfully",
	})
}

// DeletePrivateRecipe (no changes)
func (s *Store) DeletePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	query := "DELETE FROM recipes WHERE id = $1 AND submitted_by_user_id = $2 AND status = 'private'"
	res, err := s.DB.Exec(query, recipeID, userID)
	if err != nil {
		log.Printf("Error deleting private recipe: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission to delete it."})
	}

	log.Printf("User %s deleted private recipe (ID: %d)", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe deleted successfully"})
}

// SubmitPrivateRecipe (no changes)
func (s *Store) SubmitPrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	// --- MODIFIED: Also update the slug on submission ---
	// This ensures if they changed the title, the slug is fresh
	var title string
	err = s.DB.QueryRow("SELECT title FROM recipes WHERE id = $1 AND submitted_by_user_id = $2 AND status = 'private'", recipeID, userID).Scan(&title)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to find recipe."})
	}
	slug := game.Slugify(title)
	// ---

	query := `
		UPDATE recipes
		SET status = 'pending', updated_at = NOW(), slug = $1
		WHERE id = $2 AND submitted_by_user_id = $3 AND status = 'private'
	`
	res, err := s.DB.Exec(query, slug, recipeID, userID)
	if err != nil {
		log.Printf("Error submitting private recipe for review: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		// This should be rare after our check above, but good to keep
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission."})
	}

	log.Printf("User %s submitted private recipe (ID: %d) for review", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe submitted to Guild for review!"})
}

// GetFeaturedRecipes fetches all approved recipes marked as "is_featured"
func (s *Store) GetFeaturedRecipes(c echo.Context) error {
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url, r.source,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured, r.slug,
			r.prep_time_minutes, r.cook_time_minutes, r.servings
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.is_featured = TRUE AND r.status = 'approved'
		GROUP BY r.id, u.username
		ORDER BY r.updated_at DESC
	`
	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error querying featured recipes: %v\n", err)
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
			&r.AvgRating, &r.CookCount, &r.IsFeatured, &r.Slug,
			&r.PrepTimeMinutes, &r.CookTimeMinutes, &r.Servings, // Added new fields
		); err != nil {
			log.Printf("Error scanning featured recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
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

// ==================================================================
// --- NEW: IMPORT FROM URL FUNCTIONALITY ---
// ==================================================================

// --- Structs for parsing schema.org JSON-LD ---

// SchemaRecipe is the top-level struct for recipe JSON-LD
type SchemaRecipe struct {
	Type               any      `json:"@type"` // Can be string or array
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Image              any      `json:"image"` // Can be string, array of strings, or object
	RecipeIngredient   []string `json:"recipeIngredient"`
	RecipeInstructions any      `json:"recipeInstructions"` // Can be array of steps, or array of HowToSection
	PrepTime           string   `json:"prepTime"`           // e.g., "PT15M"
	CookTime           string   `json:"cookTime"`           // e.g., "PT30M"
	RecipeYield        any      `json:"recipeYield"`        // e.g., "4 servings" or ["4-6"]
}

// SchemaHowToStep is for parsing instructions
type SchemaHowToStep struct {
	Type string `json:"@type"`
	Text string `json:"text"`
}

// SchemaHowToSection is for parsing grouped instructions
type SchemaHowToSection struct {
	Type     string            `json:"@type"`
	Name     string            `json:"name"`
	ItemList []SchemaHowToStep `json:"itemListElement"`
}

// --- End Schema Structs ---

// ingredientRegex helps parse "1 1/2 cups flour"
// Group 1: Quantity (e.g., "1 1/2")
// Group 2: Unit (e.g., "cups")
// Group 3: Name (e.g., "flour")
var ingredientRegex = regexp.MustCompile(`^\s*([0-9/.\s-]+)\s*(cup|cups|oz|ounce|ounces|lb|lbs|pound|pounds|g|grams|kg|kilograms|ml|l|liter|liters|tsp|teaspoon|teaspoons|tbsp|tablespoon|tablespoons|each|pinch|dash)?\s*(.*)$`)

// parseIngredientString turns a raw string into our model
func parseIngredientString(raw string) models.Ingredient {
	raw = strings.TrimSpace(raw)
	// Simple check for section headers
	if (strings.HasPrefix(raw, "For the") && strings.HasSuffix(raw, ":")) || (strings.HasPrefix(raw, "---") && strings.HasSuffix(raw, "---")) {
		return models.Ingredient{
			Type: "header",
			Name: strings.Trim(raw, " -:"),
		}
	}

	matches := ingredientRegex.FindStringSubmatch(raw)

	if len(matches) == 4 {
		// Full match: "1 1/2 cups flour"
		unit := strings.ToLower(matches[2])
		// Normalize units
		switch unit {
		case "cups":
			unit = "cup"
		case "ounce", "ounces":
			unit = "oz"
		case "pound", "pounds":
			unit = "lb"
		case "grams":
			unit = "g"
		case "kilograms":
			unit = "kg"
		case "liter", "liters":
			unit = "l"
		case "teaspoon", "teaspoons":
			unit = "tsp"
		case "tablespoon", "tablespoons":
			unit = "tbsp"
		case "":
			unit = "each" // Default to 'each' if no unit is found
		}

		return models.Ingredient{
			Type:        "ingredient",
			QuantityStr: strings.TrimSpace(matches[1]),
			Unit:        unit,
			Name:        strings.TrimSpace(matches[3]),
		}
	}

	// Fallback: No parsable quantity/unit
	return models.Ingredient{
		Type:        "ingredient",
		QuantityStr: "",
		Unit:        "each",
		Name:        raw,
	}
}

// findJsonLd searches an HTML node for a <script type="application/ld+json">
// and returns its text content.
func findJsonLd(n *html.Node) (string, bool) {
	if n.Type == html.ElementNode && n.Data == "script" {
		var isLdJson bool
		for _, a := range n.Attr {
			if a.Key == "type" && a.Val == "application/ld+json" {
				isLdJson = true
				break
			}
		}
		if isLdJson {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					// Check if this JSON contains a "@graph"
					if strings.Contains(c.Data, "\"@graph\"") {
						return c.Data, true // Signal that this is a graph
					}
					return c.Data, false
				}
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if jsonStr, isGraph := findJsonLd(c); jsonStr != "" {
			return jsonStr, isGraph
		}
	}
	return "", false
}

// checkItemForRecipe tries to unmarshal a raw JSON message into a Recipe schema
// and returns true if it's a valid recipe.
func checkItemForRecipe(item json.RawMessage, schema *SchemaRecipe) bool {
	var typeCheck struct {
		Type any `json:"@type"`
	}
	if json.Unmarshal(item, &typeCheck) != nil {
		return false // Not a valid schema object
	}

	isRecipe := false
	if typeStr, ok := typeCheck.Type.(string); ok && strings.Contains(typeStr, "Recipe") {
		isRecipe = true
	} else if typeArr, ok := typeCheck.Type.([]interface{}); ok {
		for _, t := range typeArr {
			if str, ok := t.(string); ok && strings.Contains(str, "Recipe") {
				isRecipe = true
				break
			}
		}
	}

	if isRecipe {
		// It is a recipe! Try to unmarshal the full schema
		if json.Unmarshal(item, schema) == nil {
			return true
		}
	}
	return false
}

// --- NEW HELPER: Parse ISO 8601 Durations ---
var durationRegex = regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?`)

func parseISO8601Duration(duration string) sql.NullInt64 {
	if duration == "" {
		return sql.NullInt64{Valid: false}
	}
	matches := durationRegex.FindStringSubmatch(duration)
	if matches == nil {
		return sql.NullInt64{Valid: false}
	}

	hours := 0
	if matches[1] != "" {
		hours, _ = strconv.Atoi(matches[1])
	}
	minutes := 0
	if matches[2] != "" {
		minutes, _ = strconv.Atoi(matches[2])
	}

	totalMinutes := (hours * 60) + minutes
	if totalMinutes > 0 {
		return sql.NullInt64{Int64: int64(totalMinutes), Valid: true}
	}
	return sql.NullInt64{Valid: false}
}

// ---

// ImportRecipeFromURL handles the new import endpoint
func (s *Store) ImportRecipeFromURL(c echo.Context) error {
	// Note: We don't use userID here, but it's good that it's protected
	// so only logged-in users can use this feature.

	var req struct {
		URL string `json:"url"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request. 'url' is required."})
	}
	if req.URL == "" || !strings.HasPrefix(req.URL, "http") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "A valid URL is required."})
	}

	// 1. Fetch the URL
	client := &http.Client{}
	httpReq, err := http.NewRequest("GET", req.URL, nil)
	if err != nil {
		log.Printf("Error creating request for %s: %v", req.URL, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create request."})
	}
	// Set a user-agent to mimic a browser, as some sites block default Go user-agents
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("Error fetching URL %s: %v", req.URL, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch the URL."})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Could not access that URL. Status code: " + resp.Status})
	}

	// 2. Parse HTML to find JSON-LD
	doc, err := html.Parse(resp.Body)
	if err != nil {
		log.Printf("Error parsing HTML for %s: %v", req.URL, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to parse HTML."})
	}

	jsonStr, _ := findJsonLd(doc) // We don't need 'isGraph' here anymore, the new parser handles it
	if jsonStr == "" {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Could not find any recipe data on that page."})
	}

	// 3. Unmarshal and Map
	var schema SchemaRecipe
	var found bool

	// --- NEW PARSER LOGIC ---
	// Try to unmarshal into a single raw message first.
	var rawData json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &rawData); err != nil {
		log.Printf("Error parsing JSON-LD: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to parse structured data."})
	}

	var jsonList []json.RawMessage
	// Check if the raw data is an array `[` or an object `{`
	if len(rawData) > 0 && rawData[0] == '[' {
		// It's an array, unmarshal into a list
		if err := json.Unmarshal(rawData, &jsonList); err != nil {
			// Failed to unmarshal as array, treat as single object
			jsonList = append(jsonList, rawData)
		}
	} else if len(rawData) > 0 && rawData[0] == '{' {
		// It's a single object, add it to our list to be processed
		jsonList = append(jsonList, rawData)
	} else {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Unrecognized structured data format."})
	}

	// Now, iterate through our list of JSON objects
	for _, item := range jsonList {
		// First, check for a @graph
		var graph struct {
			Graph []json.RawMessage `json:"@graph"`
		}
		if json.Unmarshal(item, &graph) == nil && len(graph.Graph) > 0 {
			// It's a graph object, iterate the graph
			for _, graphItem := range graph.Graph {
				if checkItemForRecipe(graphItem, &schema) {
					found = true
					break
				}
			}
		} else {
			// It's a regular object, check it directly
			if checkItemForRecipe(item, &schema) {
				found = true
			}
		}

		if found {
			break
		}
	}
	// --- END NEW PARSER LOGIC ---

	if !found {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Found structured data, but could not find a 'Recipe' object."})
	}

	// 4. Create a new models.Recipe from the schema
	var newRecipe models.Recipe
	newRecipe.Title = schema.Name
	newRecipe.Description = schema.Description
	newRecipe.Source = sql.NullString{String: req.URL, Valid: true}

	// 5. Map Image --- REMOVED ---
	// We are intentionally not importing the image URL to avoid
	// copyright issues. The user will be prompted to upload their own.

	// 6. Map Times and Servings
	newRecipe.PrepTimeMinutes = parseISO8601Duration(schema.PrepTime)
	newRecipe.CookTimeMinutes = parseISO8601Duration(schema.CookTime)

	// Handle flexible 'recipeYield'
	if yieldStr, ok := schema.RecipeYield.(string); ok {
		newRecipe.Servings = sql.NullString{String: yieldStr, Valid: true}
	} else if yieldArr, ok := schema.RecipeYield.([]interface{}); ok && len(yieldArr) > 0 {
		if str, ok := yieldArr[0].(string); ok {
			newRecipe.Servings = sql.NullString{String: str, Valid: true}
		}
	}

	// 7. Map Ingredients
	newRecipe.Ingredients = []models.Ingredient{}
	for _, rawIng := range schema.RecipeIngredient {
		newRecipe.Ingredients = append(newRecipe.Ingredients, parseIngredientString(rawIng))
	}

	// 8. Map Instructions
	newRecipe.Instructions = []models.Instruction{}
	if steps, ok := schema.RecipeInstructions.([]interface{}); ok {
		for _, step := range steps {
			if stepStr, ok := step.(string); ok {
				// Simple string array
				newRecipe.Instructions = append(newRecipe.Instructions, models.Instruction{Step: stepStr})
			} else if stepMap, ok := step.(map[string]interface{}); ok {
				// Array of objects
				if stepType, ok := stepMap["@type"].(string); ok {
					if stepType == "HowToStep" {
						if text, ok := stepMap["text"].(string); ok {
							text = strings.TrimSpace(text)
							if text != "" {
								newRecipe.Instructions = append(newRecipe.Instructions, models.Instruction{Step: text})
							}
						}
					} else if stepType == "HowToSection" {
						// This is a section header
						if name, ok := stepMap["name"].(string); ok {
							// Add a header to *ingredients*
							newRecipe.Ingredients = append(newRecipe.Ingredients, models.Ingredient{Type: "header", Name: name})
						}
						// Add the steps from this section
						if itemList, ok := stepMap["itemListElement"].([]interface{}); ok {
							for _, item := range itemList {
								if itemMap, ok := item.(map[string]interface{}); ok {
									if text, ok := itemMap["text"].(string); ok {
										text = strings.TrimSpace(text)
										if text != "" {
											newRecipe.Instructions = append(newRecipe.Instructions, models.Instruction{Step: text})
										}
									}
								}
							}
						}
					}
				}
			}
		}
	} else if instStr, ok := schema.RecipeInstructions.(string); ok {
		// Just one big block of text, split by newline
		for _, line := range strings.Split(instStr, "\n") {
			if strings.TrimSpace(line) != "" {
				newRecipe.Instructions = append(newRecipe.Instructions, models.Instruction{Step: line})
			}
		}
	}

	// 9. Return the partial recipe object to the frontend for review
	log.Printf("Successfully imported recipe from %s", req.URL)
	return c.JSON(http.StatusOK, newRecipe)
}
