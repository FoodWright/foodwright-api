package store

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// CreateQuickPost allows a user to create a quick "what's cooking" post
func (s *Store) CreateQuickPost(c echo.Context) error {
	userID := c.Get("userID").(string)

	type CreatePostRequest struct {
		Content     string `json:"content"`
		ImageURL    string `json:"image_url"`
		RecipeID    *int64 `json:"recipe_id"`
		ExternalURL string `json:"external_url"`
	}
	var req CreatePostRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// Validate content
	content := strings.TrimSpace(req.Content)
	if content == "" && req.ImageURL == "" && req.ExternalURL == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Post must have content, an image, or a link"})
	}
	if len(content) > 500 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Content cannot exceed 500 characters"})
	}

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}
	defer tx.Rollback()

	// Create post
	var postID int64
	var sqlImageURL sql.NullString
	if req.ImageURL != "" {
		sqlImageURL = sql.NullString{String: req.ImageURL, Valid: true}
	}
	var sqlRecipeID sql.NullInt64
	if req.RecipeID != nil {
		sqlRecipeID = sql.NullInt64{Int64: *req.RecipeID, Valid: true}
	}
	var sqlExternalURL sql.NullString
	if req.ExternalURL != "" {
		sqlExternalURL = sql.NullString{String: req.ExternalURL, Valid: true}
	}

	query := `
		INSERT INTO posts (user_id, post_type, content, image_url, recipe_id, external_url) 
		VALUES ($1, 'quick_post', $2, $3, $4, $5) 
		RETURNING id
	`
	err = tx.QueryRow(query, userID, content, sqlImageURL, sqlRecipeID, sqlExternalURL).Scan(&postID)
	if err != nil {
		log.Printf("Error creating quick post: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create post"})
	}

	// Award 5 XP for quick post
	var currentXP int
	err = tx.QueryRow("SELECT xp FROM users WHERE id = $1", userID).Scan(&currentXP)
	if err != nil {
		log.Printf("Error getting user XP: %v\n", err)
	} else {
		newXP := currentXP + 5
		_, err = tx.Exec("UPDATE users SET xp = $1, updated_at = NOW() WHERE id = $2", newXP, userID)
		if err != nil {
			log.Printf("Error updating XP: %v\n", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing quick post transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create post"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Post created successfully",
		"post_id":    postID,
		"xp_granted": 5,
	})
}

// ShareRecipeToFeed creates a recipe_share post
func (s *Store) ShareRecipeToFeed(c echo.Context) error {
	userID := c.Get("userID").(string)
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

	// Verify recipe exists and is public
	var recipeStatus string
	err = tx.QueryRow("SELECT status FROM recipes WHERE id = $1", recipeID).Scan(&recipeStatus)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Recipe not found"})
	}
	if recipeStatus != "public" {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Cannot share private recipe"})
	}

	// Create post
	var postID int64
	err = tx.QueryRow(
		"INSERT INTO posts (user_id, post_type, recipe_id) VALUES ($1, 'recipe_share', $2) RETURNING id",
		userID, recipeID,
	).Scan(&postID)
	if err != nil {
		log.Printf("Error creating recipe share post: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to share recipe"})
	}

	// Award 10 XP for sharing recipe
	var currentXP int
	err = tx.QueryRow("SELECT xp FROM users WHERE id = $1", userID).Scan(&currentXP)
	if err != nil {
		log.Printf("Error getting user XP: %v\n", err)
	} else {
		newXP := currentXP + 10
		_, err = tx.Exec("UPDATE users SET xp = $1, updated_at = NOW() WHERE id = $2", newXP, userID)
		if err != nil {
			log.Printf("Error updating XP: %v\n", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing recipe share transaction: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to share recipe"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Recipe shared successfully",
		"post_id":    postID,
		"xp_granted": 10,
	})
}

// LikePost toggles like status on a post
func (s *Store) LikePost(c echo.Context) error {
	userID := c.Get("userID").(string)
	postIDStr := c.Param("id")
	postID, err := strconv.ParseInt(postIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid post ID"})
	}

	// Check if already liked
	var exists bool
	err = s.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = $1 AND user_id = $2)",
		postID, userID,
	).Scan(&exists)
	if err != nil {
		log.Printf("Error checking like status: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Database error"})
	}

	if exists {
		// Unlike
		_, err = s.DB.Exec("DELETE FROM post_likes WHERE post_id = $1 AND user_id = $2", postID, userID)
		if err != nil {
			log.Printf("Error unliking post: %v\n", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to unlike post"})
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Post unliked",
			"liked":   false,
		})
	} else {
		// Like
		_, err = s.DB.Exec("INSERT INTO post_likes (post_id, user_id) VALUES ($1, $2)", postID, userID)
		if err != nil {
			log.Printf("Error liking post: %v\n", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to like post"})
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Post liked",
			"liked":   true,
		})
	}
}

// RepostToFeed creates a repost of another user's post
func (s *Store) RepostToFeed(c echo.Context) error {
	userID := c.Get("userID").(string)
	postIDStr := c.Param("id")
	postID, err := strconv.ParseInt(postIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid post ID"})
	}

	// Verify post exists
	var postUserID string
	err = s.DB.QueryRow("SELECT user_id FROM posts WHERE id = $1", postID).Scan(&postUserID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Post not found"})
	}

	// Prevent reposting your own posts
	if postUserID == userID {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot repost your own post"})
	}

	// Insert repost (ON CONFLICT prevents duplicate reposts)
	_, err = s.DB.Exec(
		"INSERT INTO post_reposts (user_id, original_post_id) VALUES ($1, $2) ON CONFLICT (user_id, original_post_id) DO NOTHING",
		userID, postID,
	)
	if err != nil {
		log.Printf("Error creating repost: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to repost"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Post reposted successfully"})
}

// DeletePost allows a user to delete their own post
func (s *Store) DeletePost(c echo.Context) error {
	userID := c.Get("userID").(string)
	postIDStr := c.Param("id")
	postID, err := strconv.ParseInt(postIDStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid post ID"})
	}

	// Verify post belongs to user and is not a cook_log (cook logs should not be deleted)
	var postUserID, postType string
	err = s.DB.QueryRow("SELECT user_id, post_type FROM posts WHERE id = $1", postID).Scan(&postUserID, &postType)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Post not found"})
	}

	if postUserID != userID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "You can only delete your own posts"})
	}

	if postType == "cook_log" {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Cook log posts cannot be deleted"})
	}

	// Delete post
	_, err = s.DB.Exec("DELETE FROM posts WHERE id = $1", postID)
	if err != nil {
		log.Printf("Error deleting post: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete post"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Post deleted successfully"})
}
