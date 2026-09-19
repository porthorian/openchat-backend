package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestForwardMigrationsAndIdempotence(t *testing.T) {
	dsn := os.Getenv("OPENCHAT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCHAT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migration pass: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM openchat_schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("expected both migrations, got %d", count)
	}
}
