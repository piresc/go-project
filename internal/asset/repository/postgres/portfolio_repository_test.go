package postgres

import (
	"context"
	"errors"
	"strconv"
	"sync"
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

	if err := repo.UpsertHoldingCapped(ctx, id, "MSFT", 1, 50); err != nil {
		t.Fatalf("UpsertHoldingCapped: %v", err)
	}
	if err := repo.UpsertHoldingCapped(ctx, id, "AAPL", 2, 50); err != nil {
		t.Fatalf("UpsertHoldingCapped: %v", err)
	}
	if err := repo.UpsertHoldingCapped(ctx, id, "AAPL", 3.5, 50); err != nil {
		t.Fatalf("UpsertHoldingCapped update: %v", err)
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

	if err := repo.UpsertHoldingCapped(ctx, id, "AAPL", 1, 50); err == nil {
		t.Fatal("UpsertHoldingCapped without portfolio: want foreign key error")
	}
	if err := repo.Create(ctx, id); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpsertHoldingCapped(ctx, id, "AAPL", 0, 50); err == nil {
		t.Fatal("UpsertHoldingCapped with zero quantity: want check violation")
	}
}

func fillHoldings(t *testing.T, repo *PortfolioRepository, id string, n int) {
	t.Helper()
	ctx := context.Background()
	if err := repo.Create(ctx, id); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := repo.UpsertHoldingCapped(ctx, id, "S"+strconv.Itoa(i), 1, 50); err != nil {
			t.Fatalf("seed holding %d: %v", i, err)
		}
	}
}

func TestPortfolioRepo_UpsertHoldingCappedLimit(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "55555555-5555-5555-5555-555555555555"

	fillHoldings(t, repo, id, 50)
	if err := repo.UpsertHoldingCapped(ctx, id, "NEW", 1, 50); !errors.Is(err, usecase.ErrTooManyHoldings) {
		t.Fatalf("51st new symbol: want ErrTooManyHoldings, got %v", err)
	}
	if err := repo.UpsertHoldingCapped(ctx, id, "S0", 9, 50); err != nil {
		t.Fatalf("update existing at limit: %v", err)
	}
	got, err := repo.ListHoldings(ctx, id)
	if err != nil || len(got) != 50 {
		t.Fatalf("holdings = %d, %v; want 50", len(got), err)
	}
}

func TestPortfolioRepo_UpsertHoldingCappedConcurrent(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortfolioRepository(db)
	ctx := context.Background()
	const id = "66666666-6666-6666-6666-666666666666"

	fillHoldings(t, repo, id, 49)

	const workers = 10
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- repo.UpsertHoldingCapped(ctx, id, "N"+strconv.Itoa(i), 1, 50)
		}(i)
	}
	wg.Wait()
	close(errs)

	var ok, full int
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, usecase.ErrTooManyHoldings):
			full++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || full != workers-1 {
		t.Fatalf("succeeded=%d rejected=%d, want 1 and %d", ok, full, workers-1)
	}
	var n int
	if err := db.GetContext(ctx, &n, `SELECT COUNT(*) FROM holdings WHERE user_id = $1`, id); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 50 {
		t.Fatalf("final holdings = %d, want 50", n)
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
	if err := repo.UpsertHoldingCapped(ctx, id, "AAPL", 1, 50); err != nil {
		t.Fatalf("UpsertHoldingCapped: %v", err)
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
