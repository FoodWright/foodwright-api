package store

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FoodWright/foodwright-api/internal/game"
	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Store holds the database connection
type Store struct {
	DB *sql.DB
}

// InitDB initializes the database connection
func InitDB(connStr string) (*sql.DB, error) {
	// Load .env file
	if connStr == "" {
		godotenv.Load()
		connStr = os.Getenv("NEON_DATABASE_URL")
	}
	if connStr == "" {
		return nil, fmt.Errorf("NEON_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	log.Println("Successfully connected to the database!")
	return db, nil
}

// RunMigrations executes the database migrations
func RunMigrations(db *sql.DB, migrationsPath string) error {
	log.Println("Running database migrations...")
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migrate driver: %w", err)
	}
	if migrationsPath == "" {
		migrationsPath = "file://db/migrations"
	}
	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to init migrate instance: %w", err)
	}
	if err := m.Up(); err != nil {
		// Return the error directly
		return err
	}
	log.Println("Database migrations finished.")
	return nil
}

// ----- API Handlers -----

// HealthCheck is a simple health check endpoint
func (s *Store) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// GetRecipes handles fetching approved recipes with pagination and search
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

// GetRecipeByID handles fetching a single recipe, with security check
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

	// Security Check
	if r.Status == "private" && r.SubmittedByUserID.String != currentUserID {
		log.Printf("Access denied for recipe %d: User %s is not owner", r.ID, currentUserID)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}

	return c.JSON(http.StatusOK, r)
}

// GetCookLogsForRecipe (no changes)
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

// GetProfile (no changes)
func (s *Store) GetProfile(c echo.Context) error {
	userID := c.Get("userID").(string)
	username := c.Get("username").(string)
	log.Printf("Fetching profile for authenticated user: %s", userID)

	var profile models.UserProfile
	query := "SELECT id, username, rank, xp, badges, created_at, updated_at, is_admin FROM users WHERE id = $1"

	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.Badges,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profile.IsAdmin,
	)

	if err == sql.ErrNoRows {
		log.Printf("No profile found for user %s, creating one...\n", userID)
		profile = models.UserProfile{
			ID:        userID,
			Username:  username,
			Rank:      "Kitchen Novice",
			XP:        1,
			Badges:    []string{},
			IsAdmin:   false, // Default
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		insertQuery := `
			INSERT INTO users (id, username, rank, xp, badges, created_at, updated_at, is_admin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`
		_, err = s.DB.Exec(
			insertQuery,
			profile.ID,
			profile.Username,
			profile.Rank,
			profile.XP,
			profile.Badges,
			profile.CreatedAt,
			profile.UpdatedAt,
			profile.IsAdmin,
		)
		if err != nil {
			log.Printf("Failed to create new user profile: %v\n", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create profile"})
		}
		return c.JSON(http.StatusCreated, profile)
	} else if err != nil {
		log.Printf("Error fetching user profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch profile"})
	}

	return c.JSON(http.StatusOK, profile)
}

// GetPublicProfile (no changes)
func (s *Store) GetPublicProfile(c echo.Context) error {
	userID := c.Param("id")
	log.Printf("Fetching PUBLIC profile for user: %s", userID)

	var profile models.PublicUserProfile
	query := "SELECT id, username, rank, xp, badges, created_at FROM users WHERE id = $1"

	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.Badges,
		&profile.CreatedAt,
	)

	if err == sql.ErrNoRows {
		log.Printf("No public profile found for user %s", userID)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
	} else if err != nil {
		log.Printf("Error fetching public user profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch profile"})
	}

	return c.JSON(http.StatusOK, profile)
}

// GetPublicCookLogs (no changes)
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

// LogCook (no changes)
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
	var currentBadges pq.StringArray
	err = tx.QueryRow("SELECT xp, rank, badges FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&currentXP, &currentRank, &currentBadges)
	if err != nil {
		log.Printf("Error getting user profile for update: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to get user profile"})
	}
	newlyAwardedBadges, updatedBadgesList, err := game.CheckAndAwardBadges(tx, userID, recipeID, currentBadges)
	if err != nil {
		log.Printf("Error checking for badges: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to check for badges"})
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
		SET xp = $1, rank = $2, badges = $3, updated_at = NOW()
		WHERE id = $4
	`
	_, err = tx.Exec(updateQuery, newTotalXP, newRank, updatedBadgesList, userID)
	if err != nil {
		log.Printf("Error updating user profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update user profile"})
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save cook log"})
	}
	log.Printf("User %s logged cook. XP: %d -> %d. Rank Up: %t. Badges Awarded: %v", userID, currentXP, newTotalXP, didRankUp, newlyAwardedBadges)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":            "Cook logged successfully!",
		"xp_granted":         recipeXP,
		"new_total_xp":       newTotalXP,
		"rank_up":            didRankUp,
		"new_rank":           newRank,
		"new_badges_awarded": newlyAwardedBadges,
	})
}

// SubmitRecipe (no changes)
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

// GetMySubmissions (no changes)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// GetPendingRecipes (no changes)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
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
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	var submitterID sql.NullString
	// --- FIX: Add updated_at ---
	err = tx.QueryRow(
		"UPDATE recipes SET status = 'approved', updated_at = NOW() WHERE id = $1 RETURNING submitted_by_user_id",
		recipeID,
	).Scan(&submitterID)
	if err != nil {
		log.Printf("Error approving recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to approve recipe"})
	}
	if submitterID.Valid {
		submitterUserID := submitterID.String
		const approvalXP = 250
		const badgeName = "Recipe Smith"
		var currentBadges pq.StringArray
		err = tx.QueryRow("SELECT badges FROM users WHERE id = $1 FOR UPDATE", submitterUserID).Scan(&currentBadges)
		if err != nil {
			log.Printf("Failed to get user %s for badge award: %v", submitterUserID, err)
		} else {
			hasBadge := false
			for _, b := range currentBadges {
				if b == badgeName {
					hasBadge = true
					break
				}
			}
			if !hasBadge {
				currentBadges = append(currentBadges, badgeName)
			}
			_, err = tx.Exec(
				"UPDATE users SET xp = xp + $1, badges = $2, updated_at = NOW() WHERE id = $3",
				approvalXP, currentBadges, submitterUserID,
			)
			if err != nil {
				log.Printf("Failed to award XP/badge to user %s: %v", submitterUserID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing recipe approval: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save approval"})
	}
	log.Printf("Recipe %d approved by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe approved and XP awarded!"})
}

// RejectRecipe
func (s *Store) RejectRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}
	// --- FIX: Add updated_at ---
	_, err = s.DB.Exec("UPDATE recipes SET status = 'rejected', updated_at = NOW() WHERE id = $1", recipeID)
	if err != nil {
		log.Printf("Error rejecting recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reject recipe"})
	}
	log.Printf("Recipe %d rejected by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe rejected."})
}

// GetMyFavoriteIDs (no changes)
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

// GetMyCookbook (no changes)
func (s *Store) GetMyCookbook(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions, r.image_url
		FROM recipes r
		JOIN user_favorites f ON r.id = f.recipe_id
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE f.user_id = $1 AND r.status = 'approved'
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
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// AddFavorite (no changes)
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

// RemoveFavorite (no changes)
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
		log.Printf("Error inserting private recipe: %v\n", err)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "A recipe with this title already exists."})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}
	log.Printf("User %s created new private recipe (ID: %d)", userID, newRecipeID)
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  "Recipe saved to your private cookbook!",
		"recipeId": newRecipeID,
	})
}

// GetMyPrivateRecipes (no changes)
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
			&r.Ingredients, &r.Instructions, &r.ImageURL,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// UpdatePrivateRecipe updates a user's own private recipe
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

	// --- FIX: This is the correct query ---
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
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe updated successfully"})
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

	query := `
		UPDATE recipes
		SET status = 'pending', updated_at = NOW()
		WHERE id = $1 AND submitted_by_user_id = $2 AND status = 'private'
	`
	res, err := s.DB.Exec(query, recipeID, userID)
	if err != nil {
		log.Printf("Error submitting private recipe for review: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to submit recipe"})
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission."})
	}

	log.Printf("User %s submitted private recipe (ID: %d) for review", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe submitted to Guild for review!"})
}

// GetTags (no changes)
func (s *Store) GetTags(c echo.Context) error {
	query := `
		SELECT DISTINCT unnest(tags) AS tag 
		FROM recipes 
		WHERE status = 'approved' AND cardinality(tags) > 0
		ORDER BY tag
	`
	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error fetching tags: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch tags"})
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			log.Printf("Error scanning tag: %v", err)
			continue
		}
		tags = append(tags, tag)
	}
	return c.JSON(http.StatusOK, tags)
}

// GetRecipeComments (no changes)
func (s *Store) GetRecipeComments(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	query := `
		SELECT c.id, c.user_id, u.username, c.comment_text, c.created_at
		FROM recipe_comments c
		JOIN users u ON c.user_id = u.id
		WHERE c.recipe_id = $1
		ORDER BY c.created_at DESC
		LIMIT 100
	`
	rows, err := s.DB.Query(query, recipeID)
	if err != nil {
		log.Printf("Error querying recipe comments: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch comments"})
	}
	defer rows.Close()

	var comments []models.RecipeComment
	for rows.Next() {
		var c models.RecipeComment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Comment, &c.CreatedAt); err != nil {
			log.Printf("Error scanning comment row: %v\n", err)
			continue
		}
		comments = append(comments, c)
	}

	return c.JSON(http.StatusOK, comments)
}

// PostRecipeComment (no changes)
func (s *Store) PostRecipeComment(c echo.Context) error {
	userID := c.Get("userID").(string)
	username := c.Get("username").(string)
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	type CommentRequest struct {
		Comment string `json:"comment"`
	}
	var req CommentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}
	if strings.TrimSpace(req.Comment) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Comment cannot be empty"})
	}

	var newComment models.RecipeComment
	newComment.UserID = userID
	newComment.Username = username
	newComment.RecipeID = recipeID
	newComment.Comment = req.Comment
	newComment.CreatedAt = time.Now()

	query := `
		INSERT INTO recipe_comments (recipe_id, user_id, comment_text, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`
	err = s.DB.QueryRow(query, recipeID, userID, req.Comment, newComment.CreatedAt).Scan(&newComment.ID)
	if err != nil {
		log.Printf("Error inserting comment: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to post comment"})
	}

	return c.JSON(http.StatusCreated, newComment)
}
