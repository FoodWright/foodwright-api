package store

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// GetTags fetches a distinct, sorted list of all tags used in approved recipes.
func (s *Store) GetTags(c echo.Context) error {
	query := `
		SELECT DISTINCT unnest(tags) AS tag 
		FROM recipes 
		WHERE status = 'approved' AND cardinality(tags) > 0
		ORDER BY tag
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
