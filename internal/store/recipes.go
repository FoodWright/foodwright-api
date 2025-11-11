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
	`
	countQuery := "SELECT COUNT(DISTINCT r.id) " + baseQuery
	recipesQuery := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url
	` + baseQuery

	whereClauses := []string{"r.status = 'approved'"}
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
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
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	currentUserID, _ := c.Get("userID").(string)

	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.id = $1
	`
	log.Println("DEBUG: getRecipeByID: Executing query:", query)

	var r models.Recipe
	if err := s.DB.QueryRow(query, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
		&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
		&r.SubmittedByUsername,
		&r.Ingredients, &r.Instructions, &r.ImageURL,
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

// SubmitRecipe allows an authenticated user to submit a new recipe for review.
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

	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, req.XP, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		"pending", userID,
		sqlImageURL,
	).Scan(&newRecipeID)
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
			r.ingredients, r.instructions, r.image_url
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.submitted_by_user_id = $1
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
			r.ingredients, r.instructions, r.image_url
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.status = 'pending'
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
		); err != nil {
			log.Printf("Error scanning recipe row: pretty%v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// ApproveRecipe allows an admin to approve a pending recipe, awarding XP and a badge to the submitter.
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
		const badgeRuleKey = "RECIPE_SMITH_1"
		var badgeToAward models.Badge
		err := tx.QueryRow("SELECT id, name FROM badges WHERE rule_key = $1", badgeRuleKey).Scan(&badgeToAward.ID, &badgeToAward.Name)
		if err != nil {
			log.Printf("CRITICAL: Failed to find badge definition for pretty%s: pretty%v", badgeRuleKey, err)
		} else {
			var exists int
			err = tx.QueryRow("SELECT 1 FROM user_badges WHERE user_id = $1 AND badge_id = $2", submitterUserID, badgeToAward.ID).Scan(&exists)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("Error checking if user pretty%s has badge pretty%d: pretty%v", submitterUserID, badgeToAward.ID, err)
			} else if err == sql.ErrNoRows {
				err = game.AwardBadge(tx, submitterUserID, badgeToAward.ID)
				if err != nil {
					log.Printf("Failed to award badge pretty%s to user pretty%s: pretty%v", badgeToAward.Name, submitterUserID, err)
				}
			}
		}
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

// RejectRecipe allows an admin to reject a pending recipe.
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

// CreatePrivateRecipe allows a user to create a recipe that is not submitted for review.
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
	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id,
			image_url, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, 0, pq.Array(req.Tags), // XP is 0
		req.Ingredients, req.Instructions,
		"private", userID, // Status is 'private'
		sqlImageURL,
	).Scan(&newRecipeID)
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
			r.ingredients, r.instructions, r.image_url
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.submitted_by_user_id = $1 AND r.status = 'private'
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
		); err != nil {
			log.Printf("Error scanning recipe row: pretty%v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// UpdatePrivateRecipe allows a user to update their own private or rejected recipes.
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

	query := `
		UPDATE recipes
		SET 
			title = $1, description = $2, tags = $3,
			ingredients = $4, instructions = $5, updated_at = NOW(),
			image_url = $6
		WHERE id = $7 AND submitted_by_user_id = $8 AND (status = 'private' OR status = 'rejected')
	`
	res, err := s.DB.Exec(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		sqlImageURL,
		recipeID, userID,
	)
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

// DeletePrivateRecipe allows a user to delete their own private recipe.
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

// SubmitPrivateRecipe allows a user to change a private recipe's status to 'pending' for review.
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
		WHERE id = $1 AND submitted_by_user_id = $2 AND status = 'private'
	`
	res, err := s.DB.Exec(query, recipeID, userID)
	if err != nil {
		log.Printf("Error submitting private recipe for review: pretty%v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission."})
	}

	log.Printf("User pretty%s submitted private recipe (ID: pretty%d) for review", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe submitted to Guild for review!"})
}
