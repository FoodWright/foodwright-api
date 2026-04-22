package store

import (
	"database/sql"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetFeed returns the authenticated user's personalized feed (posts from followed users)
func (s *Store) GetFeed(c echo.Context) error {
	userID := c.Get("userID").(string)
	pageStr := c.QueryParam("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	const limit = 20
	offset := (page - 1) * limit

	query := `
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.created_at,
			COUNT(DISTINCT pl.user_id) as like_count,
			COUNT(DISTINCT pr.id) as repost_count,
			EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $1) as is_liked,
			EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = $1) as is_reposted
		FROM posts p
		JOIN users u ON p.user_id = u.id
		JOIN user_follows uf ON p.user_id = uf.followed_id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		LEFT JOIN post_likes pl ON p.id = pl.post_id
		LEFT JOIN post_reposts pr ON p.id = pr.original_post_id
		WHERE uf.follower_id = $1
		GROUP BY p.id, u.username, u.rank, r.title, r.slug, r.image_url, cl.rating, cl.notes
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.DB.Query(query, userID, limit, offset)
	if err != nil {
		log.Printf("Error querying feed: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch feed"})
	}
	defer rows.Close()

	posts := s.scanPosts(rows)

	// Count total posts for pagination
	var totalPosts int
	err = s.DB.QueryRow(`
		SELECT COUNT(p.id)
		FROM posts p
		JOIN user_follows uf ON p.user_id = uf.followed_id
		WHERE uf.follower_id = $1
	`, userID).Scan(&totalPosts)
	if err != nil {
		log.Printf("Error counting feed posts: %v\n", err)
	}

	totalPages := int(math.Ceil(float64(totalPosts) / float64(limit)))
	if totalPages == 0 {
		totalPages = 1
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"posts":        posts,
		"total_pages":  totalPages,
		"current_page": page,
	})
}

// GetExploreFeed returns all public posts (not filtered by follows)
func (s *Store) GetExploreFeed(c echo.Context) error {
	pageStr := c.QueryParam("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	const limit = 20
	offset := (page - 1) * limit

	// Check if user is authenticated for like/repost status
	userID, _ := c.Get("userID").(string)
	userIDParam := sql.NullString{Valid: false}
	if userID != "" {
		userIDParam = sql.NullString{String: userID, Valid: true}
	}

	query := `
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.created_at,
			COUNT(DISTINCT pl.user_id) as like_count,
			COUNT(DISTINCT pr.id) as repost_count,
			CASE WHEN $1::VARCHAR IS NOT NULL
				THEN EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $1)
				ELSE FALSE END as is_liked,
			CASE WHEN $1::VARCHAR IS NOT NULL
				THEN EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = $1)
				ELSE FALSE END as is_reposted
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		LEFT JOIN post_likes pl ON p.id = pl.post_id
		LEFT JOIN post_reposts pr ON p.id = pr.original_post_id
		WHERE (r.status = 'public' OR r.status IS NULL)
		GROUP BY p.id, u.username, u.rank, r.title, r.slug, r.image_url, cl.rating, cl.notes
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.DB.Query(query, userIDParam, limit, offset)
	if err != nil {
		log.Printf("Error querying explore feed: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch explore feed"})
	}
	defer rows.Close()

	posts := s.scanPosts(rows)

	// Count total posts for pagination
	var totalPosts int
	err = s.DB.QueryRow(`
		SELECT COUNT(p.id)
		FROM posts p
		LEFT JOIN recipes r ON p.recipe_id = r.id
		WHERE (r.status = 'public' OR r.status IS NULL)
	`).Scan(&totalPosts)
	if err != nil {
		log.Printf("Error counting explore posts: %v\n", err)
	}

	totalPages := int(math.Ceil(float64(totalPosts) / float64(limit)))
	if totalPages == 0 {
		totalPages = 1
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"posts":        posts,
		"total_pages":  totalPages,
		"current_page": page,
	})
}

// GetUserPosts returns all posts by a specific user
func (s *Store) GetUserPosts(c echo.Context) error {
	targetUserID := c.Param("id")
	pageStr := c.QueryParam("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	const limit = 20
	offset := (page - 1) * limit

	// Check if user is authenticated for like/repost status
	userID, _ := c.Get("userID").(string)
	userIDParam := sql.NullString{Valid: false}
	if userID != "" {
		userIDParam = sql.NullString{String: userID, Valid: true}
	}

	query := `
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.created_at,
			COUNT(DISTINCT pl.user_id) as like_count,
			COUNT(DISTINCT pr.id) as repost_count,
			CASE WHEN $1::VARCHAR IS NOT NULL
				THEN EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = $1)
				ELSE FALSE END as is_liked,
			CASE WHEN $1::VARCHAR IS NOT NULL
				THEN EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = $1)
				ELSE FALSE END as is_reposted
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		LEFT JOIN post_likes pl ON p.id = pl.post_id
		LEFT JOIN post_reposts pr ON p.id = pr.original_post_id
		WHERE p.user_id = $2 AND (r.status = 'public' OR r.status IS NULL)
		GROUP BY p.id, u.username, u.rank, r.title, r.slug, r.image_url, cl.rating, cl.notes
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := s.DB.Query(query, userIDParam, targetUserID, limit, offset)
	if err != nil {
		log.Printf("Error querying user posts: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch user posts"})
	}
	defer rows.Close()

	posts := s.scanPosts(rows)

	// Count total posts for pagination
	var totalPosts int
	err = s.DB.QueryRow(`
		SELECT COUNT(p.id)
		FROM posts p
		LEFT JOIN recipes r ON p.recipe_id = r.id
		WHERE p.user_id = $1 AND (r.status = 'public' OR r.status IS NULL)
	`, targetUserID).Scan(&totalPosts)
	if err != nil {
		log.Printf("Error counting user posts: %v\n", err)
	}

	totalPages := int(math.Ceil(float64(totalPosts) / float64(limit)))
	if totalPages == 0 {
		totalPages = 1
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"posts":        posts,
		"total_pages":  totalPages,
		"current_page": page,
	})
}

// scanPosts is a helper function to scan post rows
func (s *Store) scanPosts(rows *sql.Rows) []models.Post {
	var posts []models.Post
	for rows.Next() {
		var p models.Post
		err := rows.Scan(
			&p.ID, &p.UserID, &p.Username, &p.UserRank, &p.PostType,
			&p.RecipeID, &p.RecipeTitle, &p.RecipeSlug, &p.RecipeImage,
			&p.CookLogID, &p.Rating, &p.Notes,
			&p.Content, &p.CreatedAt,
			&p.LikeCount, &p.RepostCount, &p.IsLiked, &p.IsReposted,
		)
		if err != nil {
			log.Printf("Error scanning post row: %v\n", err)
			continue
		}
		posts = append(posts, p)
	}

	if posts == nil {
		posts = []models.Post{}
	}

	return posts
}
