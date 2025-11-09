package main

import (
	"context"
	"database/sql" // Import for parsing request body
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lib/pq"
	_ "github.com/lib/pq" // Postgres driver
	"google.golang.org/api/option"
)

// Store now includes the Firebase Auth client
type Store struct {
	DB     *sql.DB
	FBAuth *auth.Client
}

// ----- Main Function: Server Setup -----
func main() {
	// 1. Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// 2. Initialize Firebase
	fbAuth, err := initFirebase()
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	// 3. Initialize Database
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 4. Run Migrations
	err = runMigrations(db)
	if err != nil && err != migrate.ErrNoChange {
		log.Printf("Migration error: %v", err) // Use Printf, don't Fatal
	} else if err == migrate.ErrNoChange {
		log.Println("Database migrations: no change.")
	}

	// 5. Create our Store
	store := &Store{
		DB:     db,
		FBAuth: fbAuth,
	}

	// 6. Initialize Echo
	e := echo.New()

	// ----- Middleware -----
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"http://localhost:9000"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
		AllowMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete},
	}))

	// ----- API Routes -----
	e.GET("/", store.healthCheck)

	api := e.Group("/api")

	// --- Public Routes ---
	api.GET("/recipes", store.getRecipes)
	api.GET("/recipes/:id/logs", store.getCookLogsForRecipe)
	api.GET("/recipes/:id", store.getRecipeByID)

	// --- Protected Routes (Requires Auth) ---
	protected := api.Group("")
	protected.Use(store.firebaseAuthMiddleware)

	protected.GET("/profile", store.getProfile)
	protected.POST("/recipes/:id/log", store.logCook)
	protected.POST("/recipes", store.submitRecipe)
	protected.GET("/recipes/my-submissions", store.getMySubmissions)

	// --- NEW: Admin Routes (Requires Auth + Admin Middleware) ---
	admin := api.Group("/admin")
	admin.Use(store.firebaseAuthMiddleware) // First, check if they're a user
	admin.Use(store.adminAuthMiddleware)    // THEN, check if they're an admin

	admin.GET("/pending-recipes", store.getPendingRecipes)
	admin.POST("/recipes/:id/approve", store.approveRecipe)
	admin.POST("/recipes/:id/reject", store.rejectRecipe)

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default port
	}
	log.Printf("Starting server on port %s", port)
	e.Logger.Fatal(e.Start(":" + port))
}

// ----- Firebase Initialization -----
func initFirebase() (*auth.Client, error) {
	keyFilePath := os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH")
	if keyFilePath == "" {
		keyFilePath = "serviceAccountKey.json"
	}
	opt := option.WithCredentialsFile(keyFilePath)
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil, fmt.Errorf("error initializing Firebase app: %w", err)
	}
	client, err := app.Auth(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error getting Firebase Auth client: %w", err)
	}
	log.Println("Successfully connected to Firebase Auth")
	return client, nil
}

// ----- Database Initialization -----
func initDB() (*sql.DB, error) {
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
func runMigrations(db *sql.DB) error {
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
		return err
	}
	log.Println("Database migrations finished.")
	return nil
}

// ----- Auth Middleware -----
func (s *Store) firebaseAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Authorization header required"})
		}
		idToken := strings.TrimSpace(strings.Replace(authHeader, "Bearer ", "", 1))
		if idToken == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid auth token"})
		}
		token, err := s.FBAuth.VerifyIDToken(context.Background(), idToken)
		if err != nil {
			log.Printf("Error verifying token: %v\n", err)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid auth token"})
		}
		username, ok := token.Claims["name"].(string)
		if !ok || username == "" {
			username = "Guild Member"
		}
		c.Set("userID", token.UID)
		c.Set("username", username)
		return next(c)
	}
}

// --- NEW: Admin Auth Middleware ---
func (s *Store) adminAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID := c.Get("userID").(string)
		var isAdmin bool
		err := s.DB.QueryRow("SELECT is_admin FROM users WHERE id = $1", userID).Scan(&isAdmin)
		if err != nil {
			log.Printf("Admin check failed for user %s: %v", userID, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Error checking user permissions"})
		}

		if !isAdmin {
			log.Printf("Admin access denied for user %s", userID)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
		}

		// User is an admin, proceed to the handler
		c.Set("isAdmin", true)
		return next(c)
	}
}

// ----- API Handlers -----

func (s *Store) healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// --- Recipe struct ---
type Recipe struct {
	ID                  int64          `json:"id"`
	Title               string         `json:"title"`
	Description         string         `json:"description"`
	XP                  int            `json:"xp"`
	Tags                pq.StringArray `json:"tags"`
	CreatedAt           time.Time      `json:"created_at"`
	Status              string         `json:"status"`
	SubmittedByUserID   sql.NullString `json:"submitted_by_user_id"`
	SubmittedByUsername sql.NullString `json:"submitted_by_username"` // <-- NEW
}

// getRecipes
func (s *Store) getRecipes(c echo.Context) error {
	query := `
		SELECT r.id, r.title, r.description, r.xp, r.tags, r.created_at, r.status, r.submitted_by_user_id, u.username AS submitted_by_username
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

	var recipes []Recipe
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
			&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
			&r.SubmittedByUsername, // <-- NEW
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// getRecipeByID
func (s *Store) getRecipeByID(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	// --- Use explicit Prepare to avoid caching issues ---
	query := `
		SELECT r.id, r.title, r.description, r.xp, r.tags, r.created_at, r.status, r.submitted_by_user_id, u.username AS submitted_by_username
		FROM recipes r
		LEFT JOIN users u ON r.submitted_by_user_id = u.id
		WHERE r.id = $1
	`
	log.Println("DEBUG: getRecipeByID: Preparing query:", query)

	stmt, err := s.DB.Prepare(query)
	if err != nil {
		log.Printf("Error preparing statement: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer stmt.Close()

	var r Recipe
	log.Println("DEBUG: getRecipeByID: Executing prepared statement and scanning into 9 variables")

	if err := stmt.QueryRow(id).Scan(
		&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
		&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
		&r.SubmittedByUsername, // <-- NEW
	); err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
		}
		log.Printf("Error scanning recipe from prepared statement: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}

	return c.JSON(http.StatusOK, r)
}

// --- CookLog Structs ---
type DBUserCookLog struct {
	ID        int64
	UserID    string
	Username  string
	Rating    sql.NullInt64
	Notes     sql.NullString
	CreatedAt time.Time
}
type CleanCookLog struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Rating    *int64    `json:"rating"`
	Notes     *string   `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

// getCookLogsForRecipe
func (s *Store) getCookLogsForRecipe(c echo.Context) error {
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

	var dbLogs []DBUserCookLog
	for rows.Next() {
		var l DBUserCookLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Rating, &l.Notes, &l.CreatedAt); err != nil {
			log.Printf("Error scanning cook log row: %v\n", err)
			continue
		}
		dbLogs = append(dbLogs, l)
	}

	cleanLogs := make([]CleanCookLog, len(dbLogs))
	for i, dbLog := range dbLogs {
		cleanLogs[i] = CleanCookLog{
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

// --- UserProfile struct ---
type UserProfile struct {
	ID        string         `json:"id"`
	Username  string         `json:"username"`
	Rank      string         `json:"rank"`
	XP        int            `json:"xp"`
	Badges    pq.StringArray `json:"badges"`
	IsAdmin   bool           `json:"is_admin"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// getProfile (Protected)
func (s *Store) getProfile(c echo.Context) error {
	userID := c.Get("userID").(string)
	username := c.Get("username").(string)
	log.Printf("Fetching profile for authenticated user: %s", userID)

	var profile UserProfile
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
		profile.ID = userID
		profile.Username = username
		profile.Rank = "Kitchen Novice"
		profile.XP = 1
		profile.Badges = []string{}
		profile.IsAdmin = false // Default
		profile.CreatedAt = time.Now()
		profile.UpdatedAt = time.Now()

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

// --- calculateRank ---
func calculateRank(xp int) string {
	if xp >= 500 {
		return "Master Foodwright"
	}
	if xp >= 300 {
		return "Chef de Cuisine"
	}
	if xp >= 150 {
		return "Sous Chef"
	}
	if xp >= 75 {
		return "Journeyman"
	}
	if xp >= 25 {
		return "Apprentice Cook"
	}
	return "Kitchen Novice"
}

// --- checkAndAwardBadges ---
func checkAndAwardBadges(tx *sql.Tx, userID string, recipeID int64, currentBadges pq.StringArray) ([]string, pq.StringArray, error) {
	newlyAwarded := []string{}
	updatedBadges := currentBadges
	hasBadge := func(badgeName string) bool {
		for _, b := range currentBadges {
			if b == badgeName {
				return true
			}
		}
		return false
	}

	// Badge 1: "First Cook"
	var totalCooks int
	err := tx.QueryRow("SELECT COUNT(*) FROM user_cooks_log WHERE user_id = $1", userID).Scan(&totalCooks)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count total logs: %w", err)
	}
	if totalCooks == 0 && !hasBadge("First Cook") {
		newlyAwarded = append(newlyAwarded, "First Cook")
		updatedBadges = append(updatedBadges, "First Cook")
	}

	// Badge 2: "Baker"
	const bakingTarget = 3
	if !hasBadge("Baker") {
		var bakingCooks int
		query := `
			SELECT COUNT(DISTINCT l.recipe_id)
			FROM user_cooks_log l
			JOIN recipes r ON l.recipe_id = r.id
			WHERE l.user_id = $1 AND r.tags @> ARRAY['baking']::text[]
		`
		err := tx.QueryRow(query, userID).Scan(&bakingCooks)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to count baking logs: %w", err)
		}
		var isCurrentCookBaking bool
		err = tx.QueryRow("SELECT tags @> ARRAY['baking']::text[] FROM recipes WHERE id = $1", recipeID).Scan(&isCurrentCookBaking)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check current recipe tags: %w", err)
		}
		if isCurrentCookBaking && bakingCooks == (bakingTarget-1) {
			newlyAwarded = append(newlyAwarded, "Baker")
			updatedBadges = append(updatedBadges, "Baker")
		}
	}
	return newlyAwarded, updatedBadges, nil
}

// --- logCook (Protected) ---
func (s *Store) logCook(c echo.Context) error {
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

	// 3. Log this cook
	_, err = tx.Exec(
		"INSERT INTO user_cooks_log (user_id, recipe_id, notes, rating) VALUES ($1, $2, $3, $4)",
		userID, recipeID, sqlNotes, sqlRating,
	)
	if err != nil {
		log.Printf("Error inserting cook log: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to log cook record"})
	}

	// 4. Calculate new XP and Rank
	newTotalXP := currentXP + recipeXP
	newRank := calculateRank(newTotalXP)
	didRankUp := newRank != currentRank

	// 5. Check for badges
	newlyAwardedBadges, updatedBadgesList, err := checkAndAwardBadges(tx, userID, recipeID, currentBadges)
	if err != nil {
		log.Printf("Error checking for badges: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to check for badges"})
	}

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

// --- submitRecipe (Protected) ---
func (s *Store) submitRecipe(c echo.Context) error {
	userID := c.Get("userID").(string)
	type SubmitRecipeRequest struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		XP          int      `json:"xp"`
		Tags        []string `json:"tags"`
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

	query := `
		INSERT INTO recipes (title, description, xp, tags, status, submitted_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var newRecipeID int64
	err := s.DB.QueryRow(
		query,
		req.Title, req.Description, req.XP, pq.Array(req.Tags),
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

// --- getMySubmissions (Protected) ---
func (s *Store) getMySubmissions(c echo.Context) error {
	userID := c.Get("userID").(string)
	query := `
		SELECT r.id, r.title, r.description, r.xp, r.tags, r.created_at, r.status, r.submitted_by_user_id, u.username AS submitted_by_username
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

	var recipes []Recipe
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
			&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
			&r.SubmittedByUsername, // <-- NEW
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// --- NEW ADMIN HANDLER: getPendingRecipes ---
func (s *Store) getPendingRecipes(c echo.Context) error {
	query := `
		SELECT r.id, r.title, r.description, r.xp, r.tags, r.created_at, r.status, r.submitted_by_user_id, u.username AS submitted_by_username
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

	var recipes []Recipe
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.XP, &r.Tags,
			&r.CreatedAt, &r.Status, &r.SubmittedByUserID,
			&r.SubmittedByUsername, // <-- NEW
		); err != nil {
			log.Printf("Error scanning recipe row: %v\n", err)
			continue
		}
		recipes = append(recipes, r)
	}
	return c.JSON(http.StatusOK, recipes)
}

// --- NEW ADMIN HANDLER: approveRecipe ---
func (s *Store) approveRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	// --- Use a Transaction ---
	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	// 1. Update the recipe status to 'approved' and get the submitter's ID
	var submitterID sql.NullString
	err = tx.QueryRow(
		"UPDATE recipes SET status = 'approved' WHERE id = $1 RETURNING submitted_by_user_id",
		recipeID,
	).Scan(&submitterID)
	if err != nil {
		log.Printf("Error approving recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to approve recipe"})
	}

	// 2. If the recipe was submitted by a user, award them XP and a badge
	if submitterID.Valid {
		submitterUserID := submitterID.String
		const approvalXP = 250 // Big XP bonus!
		const badgeName = "Recipe Smith"

		// Get user's current badges
		var currentBadges pq.StringArray
		err = tx.QueryRow("SELECT badges FROM users WHERE id = $1 FOR UPDATE", submitterUserID).Scan(&currentBadges)
		if err != nil {
			log.Printf("Failed to get user %s for badge award: %v", submitterUserID, err)
			// Don't fail the whole transaction, just log the error
		} else {
			// Check if they already have the badge
			hasBadge := false
			for _, b := range currentBadges {
				if b == badgeName {
					hasBadge = true
					break
				}
			}
			// Add badge if they don't have it
			if !hasBadge {
				currentBadges = append(currentBadges, badgeName)
			}
			// Update user's XP and Badges
			_, err = tx.Exec(
				"UPDATE users SET xp = xp + $1, badges = $2, updated_at = NOW() WHERE id = $3",
				approvalXP, currentBadges, submitterUserID,
			)
			if err != nil {
				log.Printf("Failed to award XP/badge to user %s: %v", submitterUserID, err)
				// Don't fail the transaction, just log
			}
		}
	}

	// 3. Commit the transaction
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing recipe approval: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save approval"})
	}

	log.Printf("Recipe %d approved by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe approved and XP awarded!"})
}

// --- NEW ADMIN HANDLER: rejectRecipe ---
func (s *Store) rejectRecipe(c echo.Context) error {
	recipeIDStr := c.Param("id")
	recipeID, err := strconv.ParseInt(recipeIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid recipe ID"})
	}

	// Just update the status. We don't need a transaction.
	_, err = s.DB.Exec("UPDATE recipes SET status = 'rejected' WHERE id = $1", recipeID)
	if err != nil {
		log.Printf("Error rejecting recipe %d: %v", recipeID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to reject recipe"})
	}

	log.Printf("Recipe %d rejected by admin %s", recipeID, c.Get("userID"))
	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe rejected."})
}
