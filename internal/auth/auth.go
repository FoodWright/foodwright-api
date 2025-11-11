package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"google.golang.org/api/option"
)

// AuthHandler holds the clients needed for auth
type AuthHandler struct {
	FBAuth *auth.Client
	DB     *sql.DB
}

// InitFirebase initializes the Firebase app and auth client
func InitFirebase(keyFilePath string) (*auth.Client, error) {
	// if keyFilePath == "" {
	// 	keyFilePath = "serviceAccountKey.json"
	// }
	// if os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH") == "" {
	// 	if err := godotenv.Load(); err != nil {
	// 		log.Println("No .env file found or error loading it, relying on environment variables.")
	// 	}
	// 	keyFilePath = os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH")
	// 	if keyFilePath == "" {
	// 		keyFilePath = "serviceAccountKey.json"
	// 	}
	// }
	var opt option.ClientOption
	var app *firebase.App
	var err error

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it, relying on environment variables.")
	}

	// If the key path is provided (for local dev), use it.
	if os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH") != "" {
		log.Println("Initializing Firebase with service account key file.")
		opt = option.WithCredentialsFile(os.Getenv("FIREBASE_SERVICE_ACCOUNT_KEY_PATH"))
		app, err = firebase.NewApp(context.Background(), nil, opt)
	} else {
		// If running in GCP (e.g., Cloud Run), use Application Default Credentials.
		// The SDK will automatically find the service account attached to the environment.
		log.Println("Initializing Firebase with Application Default Credentials.")
		app, err = firebase.NewApp(context.Background(), nil)
	}

	// opt := option.WithCredentialsFile(keyFilePath)
	// app, err := firebase.NewApp(context.Background(), nil, opt)
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

// FirebaseMiddleware is a standard auth middleware that requires a valid token
func (h *AuthHandler) FirebaseMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Authorization header required"})
		}
		idToken := strings.TrimSpace(strings.Replace(authHeader, "Bearer ", "", 1))
		if idToken == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid auth token"})
		}
		token, err := h.FBAuth.VerifyIDToken(context.Background(), idToken)
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

// FirebaseMiddlewareOptional tries to auth but does not fail if no token is provided
func (h *AuthHandler) FirebaseMiddlewareOptional(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get("Authorization")
		idToken := strings.TrimSpace(strings.Replace(authHeader, "Bearer ", "", 1))

		if idToken != "" {
			token, err := h.FBAuth.VerifyIDToken(context.Background(), idToken)
			if err == nil {
				c.Set("userID", token.UID)
			}
		}
		return next(c)
	}
}

// AdminMiddleware checks if the user (set by FirebaseMiddleware) is a Guild Admin
func (h *AuthHandler) AdminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, ok := c.Get("userID").(string)
		if !ok || userID == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not authenticated"})
		}

		var isAdmin bool
		// Check for Guild Admin status
		err := h.DB.QueryRow("SELECT is_admin FROM users WHERE id = $1", userID).Scan(&isAdmin)
		if err != nil {
			log.Printf("Admin check failed for user %s: %v", userID, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Error checking user permissions"})
		}

		if !isAdmin {
			log.Printf("Admin access denied for user %s", userID)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
		}

		c.Set("isAdmin", true)
		return next(c)
	}
}

// --- NEW: SiteAdminMiddleware ---
// This checks for the *highest* permission level
func (h *AuthHandler) SiteAdminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, ok := c.Get("userID").(string)
		if !ok || userID == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not authenticated"})
		}

		var isSiteAdmin bool
		// Check for Site Admin status
		err := h.DB.QueryRow("SELECT is_site_admin FROM users WHERE id = $1", userID).Scan(&isSiteAdmin)
		if err != nil {
			log.Printf("Site Admin check failed for user %s: %v", userID, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Error checking user permissions"})
		}

		if !isSiteAdmin {
			log.Printf("Site Admin access denied for user %s", userID)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Site Admin access required"})
		}

		c.Set("isSiteAdmin", true)
		return next(c)
	}
}
