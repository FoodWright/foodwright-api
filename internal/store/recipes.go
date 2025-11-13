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
			r.ingredients, r.instructions, r.image_url,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured,
			r.slug
	` + baseQuery

	// --- MODIFIED: Filter out featured recipes from the main list ---
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug, // <-- UPDATED
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

// --- NEW: GetFeaturedRecipes ---
// GetFeaturedRecipes fetches all approved recipes marked as "is_featured"
func (s *Store) GetFeaturedRecipes(c echo.Context) error {
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount, &r.IsFeatured, &r.Slug,
		); err != nil {
			log.Printf("Error scanning featured recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// GetRecipeByID fetches a single recipe by its ID, handling private recipe access control.
func (s *Store) GetRecipeByID(c echo.Context) error {
	// --- MODIFIED: Parse ID from slug ---
	idStr := c.Param("id") // This will be "9" or "9-simple-sourdough"
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
			r.ingredients, r.instructions, r.image_url,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured,
			r.slug
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.id = $1
		GROUP BY r.id, u.username
	`
	log.Println("DEBUG: getRecipeByID: Executing query:", query)

	var r models.Recipe
	if err := s.DB.QueryRow(query, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
		&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
		&r.SubmittedByUsername,
		&r.Ingredients, &r.Instructions, &r.ImageURL,
		&r.AvgRating, &r.CookCount,
		&r.IsFeatured, &r.Slug, // <-- UPDATED
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

	// --- MODIFIED: Add slug ---
	slug := game.Slugify(req.Title)
	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, created_at, updated_at,
			slug
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
		sqlImageURL,
		slug, // <-- Pass slug
	).Scan(&newRecipeID)
	// ---
	if err != nil {
		log.Printf("Error inserting submitted recipe: pretty%v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}
	log.Printf("User pretty%s submitted new recipe (ID: pretty%d)", userID, newRecipeID)
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
			r.ingredients, r.instructions, r.image_url,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured,
			r.slug
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.submitted_by_user_id = $1
		GROUP BY r.id, u.username
		ORDER BY r.created_at DESC
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying my submissions: pretty%v\n", err)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug, // <-- UPDATED
		); err != nil {
			log.Printf("Error scanning recipe row: pretty%v\n", err)
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
			r.ingredients, r.instructions, r.image_url,
			COALESCE(AVG(l.rating), 0) AS avg_rating,
			COUNT(l.id) AS cook_count,
			r.is_featured,
			r.slug
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.status = 'pending'
		GROUP BY r.id, u.username
		ORDER BY r.created_at ASC
	`

	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error querying pending recipes: pretty%v\n", err)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug, // <-- UPDATED
		); err != nil {
			log.Printf("Error scanning recipe row: pretty%v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// ApproveRecipe
func (s *Store) ApproveRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: pretty%v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	var submitterID sql.NullString
	err = tx.QueryRow(
		"UPDATE recipes SET status = 'approved', updated_at = NOW() WHERE id = $1 RETURNING submitted_by_user_id",
		recipeID,
	).Scan(&submitterID)
	if err != nil {
		log.Printf("Error approving recipe pretty%d: pretty%v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to approve recipe"})
	}

	if submitterID.Valid {
		submitterUserID := submitterID.String
		const approvalXP = 250

		// --- MODIFIED: Use new dynamic badge engine ---
		// Call the badge engine with the "on_approval" event
		newlyAwardedBadges, err := game.CheckAndAwardBadges(tx, submitterUserID, recipeID, "on_approval")
		if err != nil {
			log.Printf("Error checking for 'on_approval' badges for user %s: %v", submitterUserID, err)
		} else if len(newlyAwardedBadges) > 0 {
			log.Printf("User %s awarded new badges: %v", submitterUserID, newlyAwardedBadges)
		}
		// --- END MODIFICATION ---

		_, err = tx.Exec(
			"UPDATE users SET xp = xp + $1, updated_at = NOW() WHERE id = $2",
			approvalXP, submitterUserID,
		)
		if err != nil {
			log.Printf("Failed to award XP to user pretty%s: pretty%v", submitterUserID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing recipe approval: pretty%v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save approval"})
	}
	log.Printf("Recipe pretty%d approved by admin pretty%s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe approved and XP awarded!"})
}

// RejectRecipe
func (s *Store) RejectRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	_, err = s.DB.Exec("UPDATE recipes SET status = 'rejected', updated_at = NOW() WHERE id = $1", recipeID)
	if err != nil {
		log.Printf("Error rejecting recipe pretty%d: pretty%v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reject recipe"})
	}
	log.Printf("Recipe pretty%d rejected by admin pretty%s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe rejected."})
}

// CreatePrivateRecipe
func (s *Store) CreatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	type PrivateRecipeRequest struct {
		Title        string                  `json:"title"`
		Description  string                  `json:"description"`
		Tags         []string                `json:"tags"`
		Ingredients  models.IngredientsList  `json:"ingredients"`
		Instructions models.InstructionsList `json:"instructions"`
		ImageURL     string                  `json:"image_url"`
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

	// --- MODIFIED: Add slug ---
	slug := game.Slugify(req.Title)
	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, created_at, updated_at,
			slug
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10)
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, 0, pq.Array(req.Tags), // XP is 0
		req.Ingredients, req.Instructions,
		"private", userID, // Status is 'private'
		sqlImageURL,
		slug, // <-- Pass slug
	).Scan(&newRecipeID)
	// ---
	if err != nil {
		log.Printf("Error inserting private recipe: pretty%v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}
	log.Printf("User pretty%s created new private recipe (ID: pretty%d)", userID, newRecipeID)
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  "Recipe saved to your private cookbook!",
		"recipeId": newRecipeID,
	})
}

// GetMyPrivateRecipes fetches all recipes owned by the user with a 'private' status.
func (s *Store) GetMyPrivateRecipes(c echo.Context) error {
	userID := c.Get("userID").(string)
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
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		LEFT JOIN user_cooks_log l ON r.id = l.recipe_id
		WHERE r.submitted_by_user_id = $1 AND r.status = 'private'
		GROUP BY r.id, u.username
		ORDER BY r.created_at DESC
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying my private recipes: pretty%v\n", err)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
			&r.AvgRating, &r.CookCount,
			&r.IsFeatured, &r.Slug, // <-- UPDATED
		); err != nil {
			log.Printf("Error scanning recipe row: pretty%v\n", err)
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

	type PrivateRecipeRequest struct {
		Title        string                  `json:"title"`
		Description  string                  `json:"description"`
		Tags         []string                `json:"tags"`
		Ingredients  models.IngredientsList  `json:"ingredients"`
		Instructions models.InstructionsList `json:"instructions"`
		ImageURL     string                  `json:"image_url"`
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

	// --- MODIFIED: Add slug ---
	slug := game.Slugify(req.Title)
	query := `
		UPDATE recipes
		SET 
			title = $1, description = $2, tags = $3,
			ingredients = $4, instructions = $5, updated_at = NOW(),
			image_url = $6,
			slug = $7
		WHERE id = $8 AND submitted_by_user_id = $9 AND (status = 'private' OR status = 'rejected')
	`
	res, err := s.DB.Exec(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		sqlImageURL,
		slug, // <-- Pass slug
		recipeID, userID,
	)
	// ---
	if err != nil {
		log.Printf("Error updating private recipe: pretty%v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found or you do not have permission to edit it."})
	}

	log.Printf("User pretty%s updated private recipe (ID: pretty%d)", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe updated successfully"})
}

// DeletePrivateRecipe
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
		log.Printf("Error deleting private recipe: pretty%v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission to delete it."})
	}

	log.Printf("User pretty%s deleted private recipe (ID: pretty%d)", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe deleted successfully"})
}

// SubmitPrivateRecipe
func (s *Store) SubmitPrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	query := `
		UPDATE recipes
		SET status = 'pending', updated_at = NOW()
		WHERE id = $1 AND submitted_by_user_id = $2 AND (status = 'private' OR status = 'rejected')
	`
	res, err := s.DB.Exec(query, recipeID, userID)
	if err != nil {
		log.Printf("Error submitting private recipe for review: pretty%v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found or you do not have permission."})
	}

	log.Printf("User pretty%s submitted private recipe (ID: pretty%d) for review", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe submitted to Guild for review!"})
}

// --- NEW: ToggleRecipeFeature ---
// ToggleRecipeFeature allows a site admin to mark/unmark a recipe as featured.
func (s *Store) ToggleRecipeFeature(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	var isFeatured bool
	query := "UPDATE recipes SET is_featured = NOT is_featured, updated_at = NOW() WHERE id = $1 RETURNING is_featured"
	err = s.DB.QueryRow(query, recipeID).Scan(&isFeatured)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
		}
		log.Printf("Error toggling featured status for recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update recipe"})
	}

	log.Printf("Site Admin %s toggled featured status for recipe %d to %t", c.Get("userID"), recipeID, isFeatured)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":     "Featured status updated",
		"id":          recipeID,
		"is_featured": isFeatured,
	})
}
