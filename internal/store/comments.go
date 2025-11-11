package store

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetRecipeComments fetches the 100 most recent comments for a given recipe.
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

// PostRecipeComment allows an authenticated user to post a new comment on a recipe.
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
