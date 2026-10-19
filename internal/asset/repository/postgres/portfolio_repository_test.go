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

func TestPortfolioRepo_Holdings(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "22222222-2222-2222-2222-222222222222"

	if err := repo.Create(ctx, id); err != nil {
		t.Fatalf("Create: %v", err)
	}
	empty, err := repo.ListHoldings(ctx, id)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ListHoldings empty = %#v, %v", empty, err)
	}

	if err := repo.UpsertHolding(ctx, id, "MSFT", 1); err != nil {
		t.Fatalf("UpsertHolding: %v", err)
	}
	if err := repo.UpsertHolding(ctx, id, "AAPL", 2); err != nil {
		t.Fatalf("UpsertHolding: %v", err)
	}
	if err := repo.UpsertHolding(ctx, id, "AAPL", 3.5); err != nil {
		t.Fatalf("UpsertHolding update: %v", err)
	}
	got, err := repo.ListHoldings(ctx, id)
	if err != nil {
		t.Fatalf("ListHoldings: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "AAPL" || got[0].Quantity != 3.5 || got[1].Symbol != "MSFT" || got[1].Quantity != 1 {
		t.Fatalf("holdings = %+v", got)
	}

	if err := repo.DeleteHolding(ctx, id, "MSFT"); err != nil {
		t.Fatalf("DeleteHolding: %v", err)
	}
	if err := repo.DeleteHolding(ctx, id, "MSFT"); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("DeleteHolding missing: want ErrNotFound, got %v", err)
	}
}

func TestPortfolioRepo_HoldingConstraints(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "33333333-3333-3333-3333-333333333333"

	if err := repo.UpsertHolding(ctx, id, "AAPL", 1); err == nil {
		t.Fatal("UpsertHolding without portfolio: want foreign key error")
	}
	if err := repo.Create(ctx, id); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpsertHolding(ctx, id, "AAPL", 0); err == nil {
		t.Fatal("UpsertHolding with zero quantity: want check violation")
	}
}

func TestPortfolioRepo_PortfolioDeleteCascadesHoldings(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "44444444-4444-4444-4444-444444444444"

	if err := repo.Create(ctx, id); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpsertHolding(ctx, id, "AAPL", 1); err != nil {
		t.Fatalf("UpsertHolding: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM portfolios WHERE user_id = $1`, id); err != nil {
		t.Fatalf("delete portfolio: %v", err)
	}
	var n int
	if err := db.GetContext(ctx, &n, `SELECT COUNT(*) FROM holdings WHERE user_id = $1`, id); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("holdings after cascade = %d, want 0", n)
	}
}
