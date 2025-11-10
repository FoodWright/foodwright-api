package store

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/FoodWright/foodwright-api/internal/game"
	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	_ "github.com/lib/pq" // Postgres driver
)

// Store holds the database connection pool
type Store struct {
	DB *sql.DB
}

// NewStore creates a new Store with the db connection
func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

// ----- Database Initialization -----
func InitDB() (*sql.DB, error) {
	connStr := os.Getenv("NEON_DATABASE_URL")
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

// ----- Database Migration Runner -----
func RunMigrations(db *sql.DB) error {
	log.Println("Running database migrations...")
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migrate driver: %w", err)
	}
	migrationsPath := os.Getenv("MIGRATIONS_PATH")
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
		if err == migrate.ErrNoChange {
			log.Println("Database migrations: no change.")
			return nil
		}
		return err
	}
	log.Println("Database migrations finished.")
	return nil
}

// ----- API Handlers -----

func (s *Store) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Store) GetRecipes(c echo.Context) error {
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.status = 'approved'
		ORDER BY r.created_at DESC
	`
	rows, err := s.DB.Query(query)
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
			&r.Ingredients, &r.Instructions,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

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
			r.ingredients, r.instructions
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
		&r.Ingredients, &r.Instructions,
	); err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
		}
		log.Printf("Error scanning recipe from prepared statement: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}

	if r.Status == "private" && r.SubmittedByUserID.String != currentUserID {
		log.Printf("Access denied for recipe %d: User %s is not owner", r.ID, currentUserID)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}

	return c.JSON(http.StatusOK, r)
}

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

	// 1. Get Recipe XP
	var recipeXP int
	err = tx.QueryRow("SELECT xp FROM recipes WHERE id = $1", recipeID).Scan(&recipeXP)
	if err != nil {
		log.Printf("Error getting recipe XP: %v\n", err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}

	// 2. Get User's current profile
	var currentXP int
	var currentRank string
	var currentBadges pq.StringArray
	err = tx.QueryRow("SELECT xp, rank, badges FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&currentXP, &currentRank, &currentBadges)
	if err != nil {
		log.Printf("Error getting user profile for update: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to get user profile"})
	}

	// 3. Check for badges *before* inserting the new log
	newlyAwardedBadges, updatedBadgesList, err := game.CheckAndAwardBadges(tx, userID, recipeID, currentBadges)
	if err != nil {
		log.Printf("Error checking for badges: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to check for badges"})
	}

	// 4. Log this cook (NOW we insert)
	_, err = tx.Exec(
		"INSERT INTO user_cooks_log (user_id, recipe_id, notes, rating) VALUES ($1, $2, $3, $4)",
		userID, recipeID, sqlNotes, sqlRating,
	)
	if err != nil {
		log.Printf("Error inserting cook log: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to log cook record"})
	}

	// 5. Calculate new XP and Rank
	newTotalXP := currentXP + recipeXP
	newRank := game.CalculateRank(newTotalXP)
	didRankUp := newRank != currentRank

	// 6. Update user's profile
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

	// 7. Commit
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

func (s *Store) SubmitRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)

	type SubmitRecipeRequest struct {
		Title        string                  `json:"title"`
		Description  string                  `json:"description"`
		XP           int                     `json:"xp"`
		Tags         []string                `json:"tags"`
		Ingredients  models.IngredientsList  `json:"ingredients"`
		Instructions models.InstructionsList `json:"instructions"`
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

	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, req.XP, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
		"pending", userID,
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

func (s *Store) GetMySubmissions(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions
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
			&r.Ingredients, &r.Instructions,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// --- Admin Handlers ---
func (s *Store) GetPendingRecipes(c echo.Context) error {
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions
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
			&r.Ingredients, &r.Instructions,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

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
	err = tx.QueryRow(
		"UPDATE recipes SET status = 'approved' WHERE id = $1 RETURNING submitted_by_user_id",
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

func (s *Store) RejectRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	_, err = s.DB.Exec("UPDATE recipes SET status = 'rejected' WHERE id = $1", recipeID)
	if err != nil {
		log.Printf("Error rejecting recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reject recipe"})
	}

	log.Printf("Recipe %d rejected by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe rejected."})
}

// --- Favorite Handlers ---
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

func (s *Store) GetMyCookbook(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions
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
			&r.Ingredients, &r.Instructions,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

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

// --- NEW PRIVATE RECIPE HANDLERS ---

func (s *Store) CreatePrivateRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)

	type PrivateRecipeRequest struct {
		Title        string                  `json:"title"`
		Description  string                  `json:"description"`
		Tags         []string                `json:"tags"`
		Ingredients  models.IngredientsList  `json:"ingredients"`
		Instructions models.InstructionsList `json:"instructions"`
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

	query := `
		INSERT INTO recipes (
			title, description, xp, tags, 
			ingredients, instructions, 
			status, submitted_by_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, 0, pq.Array(req.Tags), // XP is 0 for private
		req.Ingredients, req.Instructions,
		"private", userID, // Status is 'private'
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

func (s *Store) GetMyPrivateRecipes(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT 
			r.id, r.title, r.description, r.xp, r.tags, r.created_at, 
			r.status, r.submitted_by_user_id, u.username AS submitted_by_username,
			r.ingredients, r.instructions
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
			&r.Ingredients, &r.Instructions,
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

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

	query := `
		UPDATE recipes
		SET 
			title = $1, description = $2, tags = $3,
			ingredients = $4, instructions = $5, updated_at = NOW()
		WHERE id = $6 AND submitted_by_user_id = $7 AND status = 'private'
	`
	res, err := s.DB.Exec(
		query,
		req.Title, req.Description, pq.Array(req.Tags),
		req.Ingredients, req.Instructions,
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
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Private recipe not found or you do not have permission to edit it."})
	}

	log.Printf("User %s updated private recipe (ID: %d)", userID, recipeID)
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe updated successfully"})
}

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
