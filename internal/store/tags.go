package store

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// GetTags fetches a distinct, sorted list of all *pre-defined* tags.
func (s *Store) GetTags(c echo.Context) error {
	// --- MODIFIED QUERY ---
	// Read from the new 'tags' table instead of scanning all recipes
	query := `
		SELECT name 
		FROM tags
		ORDER BY name
	`
	rows, err := s.DB.Query(query)
	if err != nil {
		log.Printf("Error fetching tags: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch tags"})
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			log.Printf("Error scanning tag: %v\n", err)
			continue
		}
		tags = append(tags, tag)
	}
	return c.JSON(http.StatusOK, tags)
}
