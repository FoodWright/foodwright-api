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
		SELECT id, username, rank, xp, created_at, updated_at,
		       is_admin, is_site_admin, unit_preference,
		       COALESCE(follower_count, 0), COALESCE(following_count, 0)
		FROM users WHERE id = $1
	`
	var followerCount, followingCount int
	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profile.IsAdmin,
		&profile.IsSiteAdmin,
		&profile.UnitPreference,
		&followerCount,
		&followingCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Profile doesn't exist, create one
			profile = models.UserProfile{
				ID:             userID,
				Username:       username,
				Rank:           "Kitchen Novice",
				XP:             1,
				Badges:         []models.Badge{},
				IsAdmin:        false,
				IsSiteAdmin:    false,
				UnitPreference: "imperial",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}

			insertQuery := `
				INSERT INTO users (id, username, rank, xp, created_at, updated_at, is_admin, is_site_admin, unit_preference)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
				profile.UnitPreference,
			)
			if err != nil {
				log.Printf("Error creating user profile: %v\n", err)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create profile"})
			}
		} else {
			log.Printf("Error fetching user profile: %v\n", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch profile"})
		}
	}

	profile.FollowerCount = followerCount
	profile.FollowingCount = followingCount

	badges, err := s.getUserBadges(userID)
	if err != nil {
		log.Printf("Error fetching badges for user %s: %v", userID, err)
		profile.Badges = []models.Badge{}
	} else {
		profile.Badges = badges
	}

	return c.JSON(http.StatusOK, profile)
}

// GetPublicProfile fetches the public profile of another user
func (s *Store) GetPublicProfile(c echo.Context) error {
	userID := c.Param("id")
	log.Printf("Fetching PUBLIC profile for user: %s", userID)

	var profile models.PublicUserProfile
	// NOTE: We don't select unit_preference here, as it's not public
	query := "SELECT id, username, rank, xp, created_at, COALESCE(follower_count, 0), COALESCE(following_count, 0) FROM users WHERE id = $1"

	var followerCount, followingCount int
	err := s.DB.QueryRow(query, userID).Scan(
		&profile.ID,
		&profile.Username,
		&profile.Rank,
		&profile.XP,
		&profile.CreatedAt,
		&followerCount,
		&followingCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
		}
		log.Printf("Error fetching public profile: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch profile"})
	}

	profile.FollowerCount = followerCount
	profile.FollowingCount = followingCount

	badges, err := s.getUserBadges(userID)
	if err != nil {
		log.Printf("Error fetching badges for user %s: %v", userID, err)
		profile.Badges = []models.Badge{}
	} else {
		profile.Badges = badges
	}

	return c.JSON(http.StatusOK, profile)
}

// UpdatePreferences allows the authenticated user to update their account preferences
func (s *Store) UpdatePreferences(c echo.Context) error {
	userID := c.Get("userID").(string)

	type UpdatePreferencesRequest struct {
		UnitPreference string `json:"unit_preference"` // "imperial" or "metric"
	}
	var req UpdatePreferencesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	if req.UnitPreference != "imperial" && req.UnitPreference != "metric" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid unit preference"})
	}

	query := "UPDATE users SET unit_preference = $1, updated_at = NOW() WHERE id = $2"
	_, err := s.DB.Exec(query, req.UnitPreference, userID)
	if err != nil {
		log.Printf("Error updating preferences for user %s: %v\n", userID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update preferences"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":         "Preferences updated",
		"unit_preference": req.UnitPreference,
	})
}
