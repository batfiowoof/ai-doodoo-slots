// Command migrate runs database migrations using goose against the embedded
// migration files. Usage: migrate [up|down|status]
package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"os"

	"github.com/ai-doodoo-slots/services/backend/db"
	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://retro:retro@localhost:55432/retrocasino?sslmode=disable"
	}
	flag.Parse()

	command := "up"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(db.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		logger.Error("set dialect", "err", err)
		os.Exit(1)
	}
	goose.SetLogger(goose.NopLogger())

	ctx := context.Background()
	switch command {
	case "up":
		err = goose.UpContext(ctx, sqlDB, "migrations")
	case "down":
		err = goose.DownContext(ctx, sqlDB, "migrations")
	case "status":
		err = goose.StatusContext(ctx, sqlDB, "migrations")
	default:
		logger.Error("unknown command", "command", command)
		os.Exit(2)
	}
	if err != nil {
		logger.Error("migration failed", "err", err)
		os.Exit(1)
	}
	logger.Info("migrations complete", "command", command)
}
