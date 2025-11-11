package store

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

// Store holds the database connection
type Store struct {
	DB *sql.DB
}

// InitDB initializes the database connection
func InitDB(connStr string) (*sql.DB, error) {
	if connStr == "" {
		godotenv.Load()
		connStr = os.Getenv("NEON_DATABASE_URL")
	}
	if connStr == "" {
		return nil, fmt.Errorf("NEON_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	log.Println("Successfully connected to the database!")
	return db, nil
}

// RunMigrations executes the database migrations
func RunMigrations(db *sql.DB, migrationsPath string) error {
	log.Println("Running database migrations...")
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migrate driver: %w", err)
	}
	if migrationsPath == "" {
		migrationsPath = "file://db/migrations"
	}
	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to init migrate instance: %w", err)
	}
	if err := m.Up(); err != nil {
		return err
	}
	log.Println("Database migrations finished.")
	return nil
}

// HealthCheck is a simple handler to confirm the service is running.
func (s *Store) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}