package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	var migrationsPath string
	var direction string
	var steps int

	flag.StringVar(&migrationsPath, "path", "migrations", "Path to migrations directory")
	flag.StringVar(&direction, "direction", "up", "Migration direction: up, down, or version")
	flag.IntVar(&steps, "steps", 0, "Number of steps to migrate (0 = all)")
	flag.Parse()

	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	// Get DATABASE_URL from environment
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Fallback to individual DB variables
		dbHost := os.Getenv("DB_HOST")
		dbPort := os.Getenv("DB_PORT")
		dbUser := os.Getenv("DB_USER")
		dbPassword := os.Getenv("DB_PASSWORD")
		dbName := os.Getenv("DB_NAME")
		dbSSLMode := os.Getenv("DB_SSLMODE")

		if dbHost == "" || dbUser == "" || dbName == "" {
			log.Fatal("DATABASE_URL or DB_* environment variables must be set")
		}

		if dbPort == "" {
			dbPort = "5432"
		}
		if dbSSLMode == "" {
			dbSSLMode = "disable"
		}

		databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			dbUser, dbPassword, dbHost, dbPort, dbName, dbSSLMode)
	}

	// Connect to database
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// Create postgres driver instance
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("Failed to create database driver: %v", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", migrationsPath),
		"postgres",
		driver,
	)
	if err != nil {
		log.Fatalf("Failed to create migrate instance: %v", err)
	}

	// Get current version
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		log.Fatalf("Failed to get current version: %v", err)
	}

	if err == migrate.ErrNilVersion {
		log.Println("Database is at: No migrations applied yet")
	} else {
		log.Printf("Current database version: %d (dirty: %v)\n", version, dirty)
	}

	// Execute migration command
	switch direction {
	case "up":
		if steps > 0 {
			log.Printf("Migrating up %d steps...\n", steps)
			if err := m.Steps(steps); err != nil && err != migrate.ErrNoChange {
				log.Fatalf("Failed to migrate up: %v", err)
			}
		} else {
			log.Println("Migrating to latest version...")
			if err := m.Up(); err != nil && err != migrate.ErrNoChange {
				log.Fatalf("Failed to migrate up: %v", err)
			}
		}
		log.Println("✅ Migration completed successfully")

	case "down":
		if steps > 0 {
			log.Printf("Migrating down %d steps...\n", steps)
			if err := m.Steps(-steps); err != nil && err != migrate.ErrNoChange {
				log.Fatalf("Failed to migrate down: %v", err)
			}
		} else {
			log.Println("Rolling back last migration...")
			if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
				log.Fatalf("Failed to migrate down: %v", err)
			}
		}
		log.Println("✅ Rollback completed successfully")

	case "force":
		if steps == 0 {
			log.Fatal("Must specify version with -steps flag for force command")
		}
		log.Printf("Forcing version to %d...\n", steps)
		if err := m.Force(steps); err != nil {
			log.Fatalf("Failed to force version: %v", err)
		}
		log.Println("✅ Version forced successfully")

	case "version":
		version, dirty, err := m.Version()
		if err != nil && err != migrate.ErrNilVersion {
			log.Fatalf("Failed to get version: %v", err)
		}
		if err == migrate.ErrNilVersion {
			log.Println("No migrations applied yet")
		} else {
			log.Printf("Current version: %d (dirty: %v)\n", version, dirty)
		}

	default:
		log.Fatalf("Invalid direction: %s. Use 'up', 'down', 'force', or 'version'", direction)
	}

	// Print final version
	version, dirty, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		log.Printf("Warning: Failed to get final version: %v", err)
	} else if err == migrate.ErrNilVersion {
		log.Println("Final database version: No migrations applied")
	} else {
		log.Printf("Final database version: %d (dirty: %v)\n", version, dirty)
	}
}
