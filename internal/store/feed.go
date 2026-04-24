package store

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/FoodWright/foodwright-api/internal/models"
	"github.com/labstack/echo/v4"
)

// GetFeed returns the authenticated user's personalized feed (their posts + followed users + followed users' reposts)
func (s *Store) GetFeed(c echo.Context) error {
	userID := c.Get("userID").(string)
	pageStr := c.QueryParam("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	const limit = 20
	offset := (page - 1) * limit

	// Optimized query to get original posts AND reposts from followed users, newest first
	query := fmt.Sprintf(`
		WITH raw_feed AS (
			-- User's own posts
			SELECT id AS post_id, created_at, NULL AS reposted_by_username
			FROM posts
			WHERE user_id = '%s'

			UNION ALL

			-- Posts from followed users
			SELECT id AS post_id, created_at, NULL AS reposted_by_username
			FROM posts
			WHERE user_id IN (SELECT followed_id FROM user_follows WHERE follower_id = '%s')

			UNION ALL

			-- Reposts from followed users
			SELECT pr.original_post_id AS post_id, pr.created_at, u.username AS reposted_by_username
			FROM post_reposts pr
			JOIN users u ON pr.user_id = u.id
			WHERE pr.user_id IN (SELECT followed_id FROM user_follows WHERE follower_id = '%s')
		),
		deduped_feed AS (
			SELECT DISTINCT ON (post_id) post_id, created_at, reposted_by_username
			FROM raw_feed
			ORDER BY post_id, created_at DESC
		)
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.image_url, p.external_url, f.created_at,
			(SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM post_reposts WHERE original_post_id = p.id) as repost_count,
			EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = '%s') as is_liked,
			EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = '%s') as is_reposted,
			f.reposted_by_username
		FROM deduped_feed f
		JOIN posts p ON f.post_id = p.id
		JOIN users u ON p.user_id = u.id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		ORDER BY f.created_at DESC
		LIMIT %d OFFSET %d
	`, userID, userID, userID, userID, userID, limit, offset)

	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error querying feed: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch feed"})
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		var repostedBy sql.NullString
		err := rows.Scan(
			&p.ID, &p.UserID, &p.Username, &p.UserRank, &p.PostType,
			&p.RecipeID, &p.RecipeTitle, &p.RecipeSlug, &p.RecipeImage,
			&p.CookLogID, &p.Rating, &p.Notes,
			&p.Content, &p.ImageURL, &p.ExternalURL, &p.CreatedAt,
			&p.LikeCount, &p.RepostCount, &p.IsLiked, &p.IsReposted,
			&repostedBy,
		)
		if err != nil {
			log.Printf("Error scanning feed row: %v\n", err)
			continue
		}
		if repostedBy.Valid {
			p.RepostedBy = &repostedBy.String
		}
		posts = append(posts, p)
	}

	if posts == nil {
		posts = []models.Post{}
	}

	// Simple count for pagination
	var totalPosts int
	err = s.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT id FROM posts WHERE user_id = '%s' OR user_id IN (SELECT followed_id FROM user_follows WHERE follower_id = '%s')
			UNION
			SELECT original_post_id FROM post_reposts WHERE user_id IN (SELECT followed_id FROM user_follows WHERE follower_id = '%s')
		) as total
	`, userID, userID, userID)).Scan(&totalPosts)

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

	userID, _ := c.Get("userID").(string)

	query := fmt.Sprintf(`
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.image_url, p.external_url, p.created_at,
			(SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM post_reposts WHERE original_post_id = p.id) as repost_count,
			EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = '%s') as is_liked,
			EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = '%s') as is_reposted,
			NULL as reposted_by_username
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		WHERE (r.status IS NULL OR r.status = 'public')
		ORDER BY p.created_at DESC
		LIMIT %d OFFSET %d
	`, userID, userID, limit, offset)

	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error querying explore: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch explore feed"})
	}
	defer rows.Close()

	posts := s.scanPosts(rows)

	var totalPosts int
	err = s.DB.QueryRow(`SELECT COUNT(id) FROM posts p LEFT JOIN recipes r ON p.recipe_id = r.id WHERE (r.status IS NULL OR r.status = 'public')`).Scan(&totalPosts)

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

	userID, _ := c.Get("userID").(string)

	query := fmt.Sprintf(`
		SELECT
			p.id, p.user_id, u.username, u.rank, p.post_type,
			p.recipe_id, r.title, r.slug, r.image_url,
			p.cook_log_id, cl.rating, cl.notes,
			p.content, p.image_url, p.external_url, p.created_at,
			(SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM post_reposts WHERE original_post_id = p.id) as repost_count,
			EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = '%s') as is_liked,
			EXISTS(SELECT 1 FROM post_reposts WHERE original_post_id = p.id AND user_id = '%s') as is_reposted,
			NULL as reposted_by_username
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN recipes r ON p.recipe_id = r.id
		LEFT JOIN user_cooks_log cl ON p.cook_log_id = cl.id
		WHERE p.user_id = '%s' AND (r.status IS NULL OR r.status = 'public' OR p.user_id = '%s')
		ORDER BY p.created_at DESC
		LIMIT %d OFFSET %d
	`, userID, userID, targetUserID, userID, limit, offset)

	rows, err := s.DB.Query(query)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch user posts"})
	}
	defer rows.Close()

	posts := s.scanPosts(rows)

	var totalPosts int
	err = s.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(p.id) FROM posts p LEFT JOIN recipes r ON p.recipe_id = r.id WHERE p.user_id = '%s' AND (r.status IS NULL OR r.status = 'public' OR p.user_id = '%s')`, targetUserID, userID)).Scan(&totalPosts)

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
		var repostedBy sql.NullString
		err := rows.Scan(
			&p.ID, &p.UserID, &p.Username, &p.UserRank, &p.PostType,
			&p.RecipeID, &p.RecipeTitle, &p.RecipeSlug, &p.RecipeImage,
			&p.CookLogID, &p.Rating, &p.Notes,
			&p.Content, &p.ImageURL, &p.ExternalURL, &p.CreatedAt,
			&p.LikeCount, &p.RepostCount, &p.IsLiked, &p.IsReposted,
			&repostedBy,
		)
		if err != nil {
			log.Printf("Error scanning post row: %v\n", err)
			continue
		}
		if repostedBy.Valid {
			p.RepostedBy = &repostedBy.String
		}
		posts = append(posts, p)
	}

	if posts == nil {
		posts = []models.Post{}
	}

	return posts
}
