package game

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/FoodWright/foodwright-api/internal/models"
)

// CalculateRank determines a user's rank based on their XP
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

// --- NEW BADGE ENGINE ---

// CheckAndAwardBadges is the new Badge Engine.
// It checks all eligible badges against the user's current actions.
func CheckAndAwardBadges(tx *sql.Tx, userID string, recipeID int64) ([]string, error) {
	newlyAwarded := []string{}

	// 1. Get all badges the user *already has*.
	currentBadgeIDs, err := getEarnedBadgeIDs(tx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current badges: %w", err)
	}

	// 2. Get all badges that are *possible* to earn right now.
	// (Milestones, or active seasonal/event badges)
	eligibleBadges, err := getEligibleBadges(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to get eligible badges: %w", err)
	}

	// 3. Loop over all eligible badges and check the rules
	for _, badge := range eligibleBadges {
		// Skip if user already has this badge
		if _, exists := currentBadgeIDs[badge.ID]; exists {
			continue
		}

		// Run the specific rule check for this badge
		earned, err := checkBadgeRule(tx, userID, recipeID, badge)
		if err != nil {
			log.Printf("Error checking badge rule %s: %v", badge.RuleKey, err)
			continue // Don't stop the whole process
		}

		if earned {
			// --- AWARD THE BADGE ---
			err := AwardBadge(tx, userID, badge.ID) // <-- Use exported function
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
func getEligibleBadges(tx *sql.Tx) ([]models.Badge, error) {
	query := `
		SELECT id, rule_key, name, description, icon_url, badge_type
		FROM badges
		WHERE 
			badge_type = 'MILESTONE' OR 
			(badge_type = 'SEASONAL' AND NOW() BETWEEN start_date AND end_date) OR
			(badge_type = 'EVENT' AND NOW() BETWEEN start_date AND end_date)
	`
	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var badges []models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(&b.ID, &b.RuleKey, &b.Name, &b.Description, &b.IconURL, &b.BadgeType); err != nil {
			return nil, err
		}
		badges = append(badges, b)
	}
	return badges, nil
}

// --- FIX: This function is now exported (AwardBadge) ---
// AwardBadge inserts the new badge into the user_badges table
func AwardBadge(tx *sql.Tx, userID string, badgeID int64) error {
	query := "INSERT INTO user_badges (user_id, badge_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	_, err := tx.Exec(query, userID, badgeID)
	return err
}

// checkBadgeRule is the main "engine" that runs the logic for each badge
func checkBadgeRule(tx *sql.Tx, userID string, recipeID int64, badge models.Badge) (bool, error) {
	// This switch statement is the core of our engine.
	// To add a new badge, you just add a new `case` here.
	switch badge.RuleKey {

	case "FIRST_COOK":
		// This logic runs *before* the new log is inserted,
		// so a count of 0 means this is the first one.
		var totalCooks int
		err := tx.QueryRow("SELECT COUNT(*) FROM user_cooks_log WHERE user_id = $1", userID).Scan(&totalCooks)
		if err != nil {
			return false, err
		}
		return totalCooks == 0, nil

	case "BAKER_3":
		// Check if the user has cooked 3 *different* baking recipes.
		const bakingTarget = 3
		var bakingCooks int
		query := `
			SELECT COUNT(DISTINCT l.recipe_id)
			FROM user_cooks_log l
			JOIN recipes r ON l.recipe_id = r.id
			WHERE l.user_id = $1 AND r.tags @> ARRAY['baking']::text[]
		`
		err := tx.QueryRow(query, userID).Scan(&bakingCooks)
		if err != nil {
			return false, err
		}

		// This logic is flawed - it doesn't account for the *current* cook.
		// A better system would pass the `recipe` object here.
		// For now, we'll check if they are *about* to hit the target.
		if bakingCooks == (bakingTarget - 1) {
			var isCurrentCookBaking bool
			err = tx.QueryRow("SELECT tags @> ARRAY['baking']::text[] FROM recipes WHERE id = $1", recipeID).Scan(&isCurrentCookBaking)
			if err != nil {
				return false, err
			}
			return isCurrentCookBaking, nil
		}
		return false, nil

	case "RECIPE_SMITH_1":
		// This badge is NOT awarded by cooking.
		// It's awarded in `ApproveRecipe` in store.go.
		// So we just return false here.
		return false, nil

	// --- Future Badges ---
	// case "HOLIDAY_2025":
	//   ... run query for holiday tags ...
	//   return true, nil

	default:
		return false, nil
	}
}
