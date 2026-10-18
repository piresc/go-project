package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/config"
	"github.com/piresc/gokit/database"
	"github.com/piresc/gokit/testkit"

	"github.com/piresc/go-project/internal/asset/migrations"
	"github.com/piresc/go-project/internal/asset/usecase"
)

func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	ctx := context.Background()

	pg, err := testkit.NewPostgresContainer(ctx, nil, "")
	if err != nil {
		t.Skipf("postgres container unavailable (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = pg.Close(context.Background()) })

	host, err := pg.Container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := pg.Container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	portNum, err := strconv.Atoi(port.Port())
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	cfg := config.DatabaseConfig{
		Host:     host,
		Port:     portNum,
		Username: "postgres",
		Password: "postgres",
		Database: "testdb",
		SSLMode:  "disable",
	}
	if err := bootstrap.RunMigrations(migrations.FS, ".", cfg); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	client, err := database.NewPostgresClient(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client.GetDB()
}

func TestPortfolioRepo_CreateTwiceOneRow(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "11111111-1111-1111-1111-111111111111"

	var _ usecase.PortfolioRepository = repo

	if _, err := repo.Get(ctx, id); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("Get before create: want ErrNotFound, got %v", err)
	}
	for i := range 2 {
		if err := repo.Create(ctx, id); err != nil {
			t.Fatalf("Create #%d: %v", i+1, err)
		}
	}
	var n int
	if err := db.GetContext(ctx, &n, `SELECT COUNT(*) FROM portfolios WHERE user_id = $1`, id); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	got, err := repo.Get(ctx, id)
	if err != nil || got.UserID != id || got.CreatedAt.IsZero() {
		t.Fatalf("Get: %+v %v", got, err)
	}
}
