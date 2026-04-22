package game

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/FoodWright/foodwright-api/internal/models"
)

// --- Slugify and Rank (No changes) ---
var (
	nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9\s-]`)
	spaceRegex           = regexp.MustCompile(`[\s-]+`)
)

func Slugify(title string) string {
	s := nonAlphanumericRegex.ReplaceAllString(title, "")
	s = spaceRegex.ReplaceAllString(s, "-")
	return strings.ToLower(s)
}

func CalculateRank(xp int) string {
	if xp >= 500 {
		return "Master Foodwright"
	}
	if xp >= 300 {
		return "Chef de Cuisine"
	}
	if xp >= 150 {
		return "Sous Chef"
	}
	if xp >= 75 {
		return "Journeyman"
	}
	if xp >= 25 {
		return "Apprentice Cook"
	}
	return "Kitchen Novice"
}

// --- NEW DYNAMIC BADGE ENGINE ---

// RuleConfig defines the structure for our JSON-based rules
type RuleConfig struct {
	Type      string `json:"type"`      // e.g., "TOTAL_COOKS", "COOKS_WITH_TAG"
	Operator  string `json:"operator"`  // e.g., "==", ">="
	Value     int    `json:"value"`     // e.g., 1, 3
	Parameter string `json:"parameter"` // e.g., "baking"
}

// CheckAndAwardBadges is the new dynamic Badge Engine.
// It checks all eligible badges that match a specific trigger event.
func CheckAndAwardBadges(tx *sql.Tx, userID string, recipeID int64, event string) ([]string, error) {
	newlyAwarded := []string{}

	// 1. Get all badges the user *already has*.
	currentBadgeIDs, err := getEarnedBadgeIDs(tx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current badges: %w", err)
	}

	// 2. Get all badges that are *possible* to earn right now
	//    that match the trigger event.
	eligibleBadges, err := getEligibleBadges(tx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to get eligible badges: %w", err)
	}

	// 3. Loop over all eligible badges and check the rules
	for _, badge := range eligibleBadges {
		// Skip if user already has this badge
		if _, exists := currentBadgeIDs[badge.ID]; exists {
			continue
		}

		// Run the dynamic rule check for this badge
		earned, err := evaluateRule(tx, userID, recipeID, badge.RuleConfig)
		if err != nil {
			log.Printf("Error evaluating badge rule %s: %v", badge.Name, err)
			continue // Don't stop the whole process
		}

		if earned {
			// --- AWARD THE BADGE ---
			err := AwardBadge(tx, userID, badge.ID)
			if err != nil {
				log.Printf("Error awarding badge %s: %v", badge.Name, err)
				continue
			}
			newlyAwarded = append(newlyAwarded, badge.Name)
			log.Printf("AWARDED BADGE: User %s earned %s", userID, badge.Name)
		}
	}

	return newlyAwarded, nil
}

// getEarnedBadgeIDs fetches a Set of badge IDs the user already has
func getEarnedBadgeIDs(tx *sql.Tx, userID string) (map[int64]bool, error) {
	rows, err := tx.Query("SELECT badge_id FROM user_badges WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, nil
}

// getEligibleBadges fetches all badges that can be earned right now
// for a specific trigger event.
func getEligibleBadges(tx *sql.Tx, event string) ([]models.Badge, error) {
	query := `
		SELECT id, name, rule_config
		FROM badges
		WHERE 
			trigger_event = $1 AND (
				badge_type = 'MILESTONE' OR 
				(badge_type = 'SEASONAL' AND NOW() BETWEEN start_date AND end_date) OR
				(badge_type = 'EVENT' AND NOW() BETWEEN start_date AND end_date)
			)
	`
	rows, err := tx.Query(query, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var badges []models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(&b.ID, &b.Name, &b.RuleConfig); err != nil {
			return nil, err
		}
		badges = append(badges, b)
	}
	return badges, nil
}

// AwardBadge inserts the new badge into the user_badges table
func AwardBadge(tx *sql.Tx, userID string, badgeID int64) error {
	query := "INSERT INTO user_badges (user_id, badge_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	_, err := tx.Exec(query, userID, badgeID)
	return err
}

// compare is a simple helper to evaluate the rule
func compare(count int, op string, value int) bool {
	switch op {
	case "==":
		return count == value
	case ">=":
		return count >= value
	case ">":
		return count > value
	case "<=":
		return count <= value
	case "<":
		return count < value
	default:
		return false
	}
}

// evaluateRule is the new engine that parses and executes JSON rules.
// NOTE: This logic assumes it's running *after* the new data is in the DB.
// (e.g., after the new cook_log is inserted).
func evaluateRule(tx *sql.Tx, userID string, recipeID int64, ruleConfig *json.RawMessage) (bool, error) {
	var config RuleConfig
	if err := json.Unmarshal(*ruleConfig, &config); err != nil {
		return false, fmt.Errorf("failed to parse rule_config: %w", err)
	}

	var count int
	var err error

	switch config.Type {
	case "TOTAL_COOKS":
		query := "SELECT COUNT(*) FROM user_cooks_log WHERE user_id = $1"
		err = tx.QueryRow(query, userID).Scan(&count)

	case "COOKS_WITH_TAG":
		if config.Parameter == "" {
			return false, errors.New("rule 'COOKS_WITH_TAG' requires a 'parameter' (tag name)")
		}
		query := `
			SELECT COUNT(DISTINCT l.recipe_id)
			FROM user_cooks_log l
			JOIN recipes r ON l.recipe_id = r.id
			WHERE l.user_id = $1 AND r.tags @> ARRAY[$2]::text[]
		`
		err = tx.QueryRow(query, userID, config.Parameter).Scan(&count)

	case "APPROVED_SUBMISSIONS":
		// Updated for new social model - recipes are published as 'public'
		query := "SELECT COUNT(*) FROM recipes WHERE submitted_by_user_id = $1 AND status = 'public'"
		err = tx.QueryRow(query, userID).Scan(&count)

	case "TOTAL_PRIVATE_RECIPES":
		query := "SELECT COUNT(*) FROM recipes WHERE submitted_by_user_id = $1 AND status = 'private'"
		err = tx.QueryRow(query, userID).Scan(&count)

	case "TOTAL_FOLLOWERS":
		query := "SELECT COALESCE(follower_count, 0) FROM users WHERE id = $1"
		err = tx.QueryRow(query, userID).Scan(&count)

	case "TOTAL_QUICK_POSTS":
		query := "SELECT COUNT(*) FROM posts WHERE user_id = $1 AND post_type = 'quick_post'"
		err = tx.QueryRow(query, userID).Scan(&count)

	case "TOTAL_RECIPE_SHARES":
		query := "SELECT COUNT(*) FROM posts WHERE user_id = $1 AND post_type = 'recipe_share'"
		err = tx.QueryRow(query, userID).Scan(&count)

	default:
		return false, fmt.Errorf("unknown rule type: %s", config.Type)
	}

	if err != nil {
		return false, fmt.Errorf("failed to execute query for rule %s: %w", config.Type, err)
	}

	// Finally, compare the result
	return compare(count, config.Operator, config.Value), nil
}
