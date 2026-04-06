package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"

	platformconfig "gloss/internal/platform/config"
)

func main() {
	command := flag.String("command", "up", "migration command: up | down | down-all | version")
	steps := flag.Int("steps", 1, "number of steps for down command")
	path := flag.String("path", "file://schema/postgres/migrations", "migration files path")
	flag.Parse()

	cfg, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := openDB(cfg)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("create migrate db driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(*path, "postgres", driver)
	if err != nil {
		log.Fatalf("create migrate instance: %v", err)
	}

	switch *command {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1 * *steps)
	case "down-all":
		err = m.Down()
	case "version":
		printVersion(m)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", *command)
		os.Exit(1)
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("run migration command %q: %v", *command, err)
	}

	log.Printf("migration command %q completed", *command)
}

func openDB(cfg platformconfig.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.DB.Host,
		cfg.DB.Port,
		cfg.DB.User,
		cfg.DB.Password,
		cfg.DB.Name,
		cfg.DB.SSLMode,
	)

	return sql.Open("pgx", dsn)
}

func printVersion(m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		log.Println("version: none")
		return
	}
	if err != nil {
		log.Fatalf("read migration version: %v", err)
	}

	log.Printf("version: %d dirty: %t\n", version, dirty)
}
