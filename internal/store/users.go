package store

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetProfile fetches the profile for the currently authenticated user, creating one if it doesn't exist.
func (s *Store) GetProfile(c echo.Context) error {
	userID := c.Get("userID").(string)
	username := c.Get("username").(string)
	log.Printf("Fetching profile for authenticated user: %s", userID)

	var profile models.UserProfile
	query := `
		SELECT id, username, rank, xp, created_at, updated_at, is_admin, is_site_admin 
		FROM users WHERE id = $1
	`
	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profile.IsAdmin,
		&profile.IsSiteAdmin,
	)

	if err == sql.ErrNoRows {
		log.Printf("No profile found for user %s, creating one...\n", userID)
		profile = models.UserProfile{
			ID:          userID,
			Username:    username,
			Rank:        "Kitchen Novice",
			XP:          1,
			Badges:      []models.Badge{},
			IsAdmin:     false,
			IsSiteAdmin: false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		insertQuery := `
			INSERT INTO users (id, username, rank, xp, created_at, updated_at, is_admin, is_site_admin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`
		_, err = s.DB.Exec(
			insertQuery,
			profile.ID,
			profile.Username,
			profile.Rank,
			profile.XP,
			profile.CreatedAt,
			profile.UpdatedAt,
			profile.IsAdmin,
			profile.IsSiteAdmin,
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

	badges, err := s.getUserBadges(userID)
	if err != nil {
		log.Printf("Error fetching badges for user %s: %v", userID, err)
		profile.Badges = []models.Badge{}
	} else {
		profile.Badges = badges
	}

	return c.JSON(http.StatusOK, profile)
}

// GetPublicProfile fetches a user's public-facing profile information.
func (s *Store) GetPublicProfile(c echo.Context) error {
	userID := c.Param("id")
	log.Printf("Fetching PUBLIC profile for user: %s", userID)

	var profile models.PublicUserProfile
	query := "SELECT id, username, rank, xp, created_at FROM users WHERE id = $1"

	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.CreatedAt,
	)

	if err == sql.ErrNoRows {
		log.Printf("No public profile found for user %s", userID)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
	} else if err != nil {
		log.Printf("Error fetching public user profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch profile"})
	}

	badges, err := s.getUserBadges(userID)
	if err != nil {
		log.Printf("Error fetching badges for user %s: %v", userID, err)
		profile.Badges = []models.Badge{}
	} else {
		profile.Badges = badges
	}

	return c.JSON(http.StatusOK, profile)
}
