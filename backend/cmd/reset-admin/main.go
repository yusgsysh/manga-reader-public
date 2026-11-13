// Command reset-admin resets an existing administrator password directly in
// the application database.
//
// It exists for the one case the HTTP API cannot solve: nobody can log in, so
// there is no platform administrator able to use the normal user-management
// endpoints. The command never creates users and never prints the password or
// its hash.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reset-admin:", err)
		os.Exit(1)
	}
}

func run() error {
	driver := flag.String("driver", envOr("MANGA_READER_DB_DRIVER", "sqlite"), "database driver: sqlite or postgres")
	path := flag.String("db", envOr("MANGA_READER_DB_PATH", "data/manga-reader.db"), "SQLite database path")
	dsn := flag.String("dsn", envOr("MANGA_READER_DB_DSN", os.Getenv("DATABASE_URL")), "PostgreSQL DSN (when -driver=postgres)")
	username := flag.String("username", "admin", "account username")
	passwordFile := flag.String("password-file", "", "read the new password from this file instead of stdin")
	flag.Parse()

	password, err := readPassword(*passwordFile)
	if err != nil {
		return err
	}
	if len(password) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}

	driverName := strings.ToLower(strings.TrimSpace(*driver))
	switch driverName {
	case "pg", "postgresql":
		driverName = "postgres"
	}
	db, err := database.NewDB(database.Options{
		Driver: database.Driver(driverName),
		DSN:    chooseDSN(*path, *dsn),
	})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := resetPassword(context.Background(), db.Client, *username, password); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "password reset for user %q\n", *username)
	return nil
}

func chooseDSN(path, dsn string) string {
	if strings.TrimSpace(dsn) != "" {
		return strings.TrimSpace(dsn)
	}
	return path
}

func readPassword(path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		return trimLine(string(data)), nil
	}
	// The password is read from stdin so it does not appear in the process
	// list or shell history. `printf '%s\n' "$PASSWORD" | ...` is the
	// recommended invocation.
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read new password from stdin: %w", err)
	}
	return trimLine(line), nil
}

func trimLine(value string) string {
	return strings.TrimRight(value, "\r\n")
}

func resetPassword(ctx context.Context, client *ent.Client, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username must not be empty")
	}

	u, err := client.User.Query().
		Where(user.UsernameEQ(username)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("user %q not found", username)
		}
		return fmt.Errorf("read user: %w", err)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := client.User.UpdateOneID(u.ID).
		SetPasswordHash(hash).
		SetStatus(config.StatusActive).
		AddTokenVersion(1).
		Save(ctx); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
