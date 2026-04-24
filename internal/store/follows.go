package store

import (
	"log"
	"net/http"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// FollowUser allows the authenticated user to follow another user
func (s *Store) FollowUser(c echo.Context) error {
	followerID := c.Get("userID").(string)
	followedID := c.Param("id")

	if followerID == followedID {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot follow yourself"})
	}

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	// Check if followed user exists
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)", followedID).Scan(&exists)
	if err != nil || !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
	}

	// Insert follow relationship (ON CONFLICT DO NOTHING prevents duplicate follows)
	_, err = tx.Exec(
		"INSERT INTO user_follows (follower_id, followed_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		followerID, followedID,
	)
	if err != nil {
		log.Printf("Error creating follow: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to follow user"})
	}

	// Update denormalized counts
	_, err = tx.Exec("UPDATE users SET following_count = following_count + 1 WHERE id = $1", followerID)
	if err != nil {
		log.Printf("Error updating following count: %v\n", err)
	}
	_, err = tx.Exec("UPDATE users SET follower_count = follower_count + 1 WHERE id = $1", followedID)
	if err != nil {
		log.Printf("Error updating follower count: %v\n", err)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing follow transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to follow user"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Successfully followed user"})
}

// UnfollowUser allows the authenticated user to unfollow another user
func (s *Store) UnfollowUser(c echo.Context) error {
	followerID := c.Get("userID").(string)
	followedID := c.Param("id")

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	// Delete follow relationship
	result, err := tx.Exec(
		"DELETE FROM user_follows WHERE follower_id = $1 AND followed_id = $2",
		followerID, followedID,
	)
	if err != nil {
		log.Printf("Error deleting follow: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to unfollow user"})
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Follow relationship not found"})
	}

	// Update denormalized counts
	_, err = tx.Exec("UPDATE users SET following_count = GREATEST(following_count - 1, 0) WHERE id = $1", followerID)
	if err != nil {
		log.Printf("Error updating following count: %v\n", err)
	}
	_, err = tx.Exec("UPDATE users SET follower_count = GREATEST(follower_count - 1, 0) WHERE id = $1", followedID)
	if err != nil {
		log.Printf("Error updating follower count: %v\n", err)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing unfollow transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to unfollow user"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Successfully unfollowed user"})
}

// GetFollowers returns a list of users who follow the specified user
func (s *Store) GetFollowers(c echo.Context) error {
	userID := c.Param("id")

	query := `
		SELECT uf.follower_id, u.username, u.rank, uf.created_at
		FROM user_follows uf
		JOIN users u ON uf.follower_id = u.id
		WHERE uf.followed_id = $1
		ORDER BY uf.created_at DESC
		LIMIT 100
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying followers: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch followers"})
	}
	defer rows.Close()

	var followers []models.Follow
	for rows.Next() {
		var f models.Follow
		if err := rows.Scan(&f.FollowerID, &f.Username, &f.Rank, &f.CreatedAt); err != nil {
			log.Printf("Error scanning follower row: %v\n", err)
			continue
		}
		f.FollowedID = userID
		followers = append(followers, f)
	}

	if followers == nil {
		followers = []models.Follow{}
	}

	return c.JSON(http.StatusOK, followers)
}

// GetFollowing returns a list of users that the specified user follows
func (s *Store) GetFollowing(c echo.Context) error {
	userID := c.Param("id")

	query := `
		SELECT uf.followed_id, u.username, u.rank, uf.created_at
		FROM user_follows uf
		JOIN users u ON uf.followed_id = u.id
		WHERE uf.follower_id = $1
		ORDER BY uf.created_at DESC
		LIMIT 100
	`

	rows, err := s.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error querying following: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch following"})
	}
	defer rows.Close()

	var following []models.Follow
	for rows.Next() {
		var f models.Follow
		if err := rows.Scan(&f.FollowedID, &f.Username, &f.Rank, &f.CreatedAt); err != nil {
			log.Printf("Error scanning following row: %v\n", err)
			continue
		}
		f.FollowerID = userID
		following = append(following, f)
	}

	if following == nil {
		following = []models.Follow{}
	}

	return c.JSON(http.StatusOK, following)
}

// CheckFollowStatus checks if the authenticated user follows the specified user
func (s *Store) CheckFollowStatus(c echo.Context) error {
	followerID := c.Get("userID").(string)
	followedID := c.Param("id")

	var exists bool
	err := s.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM user_follows WHERE follower_id = $1 AND followed_id = $2)",
		followerID, followedID,
	).Scan(&exists)

	if err != nil {
		log.Printf("Error checking follow status: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}

	return c.JSON(http.StatusOK, map[string]bool{"is_following": exists})
}
