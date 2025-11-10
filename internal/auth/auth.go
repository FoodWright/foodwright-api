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
	"github.com/labstack/echo/v4"
	"google.golang.org/api/option"
)

// AuthHandler holds the clients needed for auth
type AuthHandler struct {
	FBAuth *auth.Client
	DB     *sql.DB
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(fbAuth *auth.Client, db *sql.DB) *AuthHandler {
	return &AuthHandler{
		FBAuth: fbAuth,
		DB:     db,
	}
}

// InitFirebase initializes the Firebase app and auth client
func InitFirebase() (*auth.Client, error) {
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

// AdminMiddleware checks if the user (set by FirebaseMiddleware) is an admin
func (h *AuthHandler) AdminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, ok := c.Get("userID").(string)
		if !ok || userID == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not authenticated"})
		}

		var isAdmin bool
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
