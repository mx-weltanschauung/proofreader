package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"proofreader/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: migrate <up|down|down-all|force VERSION|version>")
	}

	command := os.Args[1]

	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%v); using environment variables", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Open database connection
	db, err := sql.Open("pgx", cfg.Database.DSN())
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create driver instance
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("Failed to create driver: %v", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithDatabaseInstance(
		"file://internal/database/migrations",
		"postgres",
		driver,
	)
	if err != nil {
		log.Fatalf("Failed to create migrate instance: %v", err)
	}

	// Execute command
	switch command {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		fmt.Println("Migrations applied successfully")

	case "down":
		// Одна ступень, не весь стек. m.Down() однажды уже стёр рабочую базу:
		// «откатить последнюю миграцию» — единственное, чего от `down` ждут.
		// На пустой базе Steps(-1) уходит искать несуществующий файл, поэтому
		// сначала спрашиваем версию.
		if _, _, err := m.Version(); err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				fmt.Println("Nothing to roll back")
				break
			}
			log.Fatalf("Failed to get version: %v", err)
		}
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("Failed to roll back one migration: %v", err)
		}
		fmt.Println("Rolled back one migration")

	case "down-all":
		// Откат ВСЕХ миграций: все таблицы базы и все данные пропадают.
		// Без явного подтверждения именем базы не запускается.
		if os.Getenv("MIGRATE_CONFIRM") != cfg.Database.Name {
			fmt.Fprintf(os.Stderr,
				"down-all откатит ВСЕ миграции и удалит все таблицы и данные базы %q.\n"+
					"Если это действительно нужно, повтори с подтверждением:\n"+
					"    MIGRATE_CONFIRM=%s go run cmd/migrate/main.go down-all\n",
				cfg.Database.Name, cfg.Database.Name)
			os.Exit(1)
		}
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Failed to roll back all migrations: %v", err)
		}
		fmt.Printf("All migrations rolled back in %q\n", cfg.Database.Name)

	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("Failed to get version: %v", err)
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)

	case "force":
		if len(os.Args) < 3 {
			log.Fatal("Usage: migrate force VERSION")
		}
		var version int
		fmt.Sscanf(os.Args[2], "%d", &version)
		if err := m.Force(version); err != nil {
			log.Fatalf("Failed to force version: %v", err)
		}
		fmt.Printf("Forced version to %d\n", version)

	default:
		log.Fatalf("Unknown command: %s", command)
	}
}
