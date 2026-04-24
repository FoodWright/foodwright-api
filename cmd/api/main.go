package main

import (
	"log"
	"net/http"
	"os"

	"github.com/FoodWright/foodwright-api/internal/auth"
	"github.com/FoodWright/foodwright-api/internal/store"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 1. Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it, relying on environment variables.")
	}

	// 2. Initialize Firebase
	fbAuth, err := auth.InitFirebase(os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH"))
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	// 3. Initialize Database
	db, err := store.InitDB(os.Getenv("NEON_DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 4. Run Migrations
	err = store.RunMigrations(db, os.Getenv("MIGRATIONS_PATH"))
	if err != nil && err.Error() != "no change" {
		log.Printf("Migration error: %v", err)
	} else if err != nil && err.Error() == "no change" {
		log.Println("Database migrations: no change.")
	}

	// 5. Create our Store and AuthHandler
	s := &store.Store{
		DB: db,
	}
	authHandler := &auth.AuthHandler{
		FBAuth: fbAuth,
		DB:     db,
	}

	// 6. Initialize Echo
	e := echo.New()

	// ----- Middleware -----
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"http://localhost:9000", "https://foodwright.com"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
		AllowMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete},
	}))

	// ----- API Routes -----
	e.GET("/", s.HealthCheck)

	api := e.Group("/api")

	// --- Public Routes ---
	api.GET("/recipes", s.GetRecipes)
	api.GET("/recipes/featured", s.GetFeaturedRecipes)
	api.GET("/recipes/:id/logs", s.GetCookLogsForRecipe)
	api.GET("/recipes/:id", s.GetRecipeByID, authHandler.FirebaseMiddlewareOptional)
	api.GET("/profile/:id", s.GetPublicProfile)
	api.GET("/profile/:id/logs", s.GetPublicCookLogs)
	api.GET("/tags", s.GetTags)
	api.GET("/recipes/:id/comments", s.GetRecipeComments)

	// --- Protected Routes (Requires Auth) ---
	protected := api.Group("")
	protected.Use(authHandler.FirebaseMiddleware)

	protected.GET("/profile", s.GetProfile)
	protected.PUT("/profile/preferences", s.UpdatePreferences)
	protected.POST("/recipes/:id/log", s.LogCook)
	protected.POST("/recipes", s.SubmitRecipe)
	protected.GET("/recipes/my-submissions", s.GetMyRecipes)

	protected.GET("/my-favorite-ids", s.GetFavoriteIDs)
	protected.GET("/my-cookbook", s.GetMyCookbook)
	protected.POST("/recipes/:id/favorite", s.AddFavorite)
	protected.DELETE("/recipes/:id/favorite", s.RemoveFavorite)

	protected.POST("/recipes/private", s.CreatePrivateRecipe)
	protected.GET("/my-private-recipes", s.GetMyRecipes)
	protected.PUT("/recipes/private/:id", s.UpdatePrivateRecipe)
	protected.DELETE("/recipes/private/:id", s.DeletePrivateRecipe)
	protected.POST("/recipes/private/:id/submit", s.SubmitPrivateRecipe)

	// --- THIS IS THE NEW LINE ---
	protected.POST("/recipes/import-url", s.ImportRecipeFromURL)
	// ----------------------------

	protected.POST("/recipes/:id/comments", s.PostRecipeComment)

	// --- Social Routes ---
	protected.POST("/users/:id/follow", s.FollowUser)
	protected.DELETE("/users/:id/follow", s.UnfollowUser)
	protected.GET("/users/:id/follow-status", s.CheckFollowStatus)
	protected.GET("/users/:id/followers", s.GetFollowers)
	protected.GET("/users/:id/following", s.GetFollowing)
	protected.GET("/feed", s.GetFeed)
	protected.POST("/posts/quick", s.CreateQuickPost)
	protected.POST("/recipes/:id/share", s.ShareRecipeToFeed)
	protected.POST("/posts/:id/like", s.LikePost)
	protected.POST("/posts/:id/repost", s.RepostToFeed)
	protected.DELETE("/posts/:id", s.DeletePost)

	// Public social routes
	api.GET("/explore", s.GetExploreFeed, authHandler.FirebaseMiddlewareOptional)
	api.GET("/users/:id/posts", s.GetUserPosts, authHandler.FirebaseMiddlewareOptional)

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s", port)
	e.Logger.Fatal(e.Start(":" + port))
}
