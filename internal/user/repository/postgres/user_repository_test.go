package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/piresc/gokit/bootstrap"
	"github.com/piresc/gokit/config"
	"github.com/piresc/gokit/database"
	"github.com/piresc/gokit/testkit"

	"github.com/piresc/go-project/internal/user/domain"
	"github.com/piresc/go-project/internal/user/migrations"
	"github.com/piresc/go-project/internal/user/usecase"
)

func newTestRepo(t *testing.T) (*UserRepository, *sqlx.DB) {
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

	return NewUserRepository(client.GetDB()), client.GetDB()
}

func TestUserRepository(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	var _ usecase.UserRepository = repo

	user := &domain.User{Email: "a@example.com", DisplayName: "Alice", AvatarURL: "http://x/a.png", Role: "investor"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.ID == "" || user.CreatedAt.IsZero() {
		t.Fatalf("Create did not populate identity: %+v", user)
	}

	t.Run("find by id and email", func(t *testing.T) {
		byID, err := repo.FindByID(ctx, user.ID)
		if err != nil || byID == nil {
			t.Fatalf("FindByID: %v %v", byID, err)
		}
		if byID.Email != user.Email || byID.DisplayName != "Alice" || byID.AvatarURL != "http://x/a.png" || byID.Role != "investor" {
			t.Fatalf("unexpected user: %+v", byID)
		}
		byEmail, err := repo.FindByEmail(ctx, user.Email)
		if err != nil || byEmail == nil || byEmail.ID != user.ID {
			t.Fatalf("FindByEmail: %+v %v", byEmail, err)
		}
	})

	t.Run("unknown returns nil nil", func(t *testing.T) {
		got, err := repo.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
		if err != nil || got != nil {
			t.Fatalf("FindByID unknown: %+v %v", got, err)
		}
		got, err = repo.FindByEmail(ctx, "nobody@example.com")
		if err != nil || got != nil {
			t.Fatalf("FindByEmail unknown: %+v %v", got, err)
		}
		got, err = repo.FindByProvider(ctx, "google", "nope")
		if err != nil || got != nil {
			t.Fatalf("FindByProvider unknown: %+v %v", got, err)
		}
		tok, err := repo.FindRefreshTokenByHash(ctx, "nope")
		if err != nil || tok != nil {
			t.Fatalf("FindRefreshTokenByHash unknown: %+v %v", tok, err)
		}
	})

	t.Run("null display_name and avatar_url read as empty", func(t *testing.T) {
		const id = "11111111-1111-1111-1111-111111111111"
		if _, err := db.ExecContext(ctx,
			`INSERT INTO users (id, email) VALUES ($1, 'null@example.com')`, id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		got, err := repo.FindByID(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("FindByID: %+v %v", got, err)
		}
		if got.DisplayName != "" || got.AvatarURL != "" {
			t.Fatalf("expected empty strings, got %+v", got)
		}
	})

	t.Run("auth provider lookup", func(t *testing.T) {
		if err := repo.CreateAuthProvider(ctx, &domain.AuthProvider{
			UserID: user.ID, Provider: "google", ProviderSubject: "sub-1",
		}); err != nil {
			t.Fatalf("CreateAuthProvider: %v", err)
		}
		got, err := repo.FindByProvider(ctx, "google", "sub-1")
		if err != nil || got == nil || got.ID != user.ID {
			t.Fatalf("FindByProvider: %+v %v", got, err)
		}
	})

	t.Run("update display name", func(t *testing.T) {
		if err := repo.UpdateDisplayName(ctx, user.ID, "Alicia"); err != nil {
			t.Fatalf("UpdateDisplayName: %v", err)
		}
		got, err := repo.FindByID(ctx, user.ID)
		if err != nil || got == nil || got.DisplayName != "Alicia" {
			t.Fatalf("after update: %+v %v", got, err)
		}
	})

	t.Run("refresh tokens", func(t *testing.T) {
		exp := time.Now().UTC().Add(time.Hour)
		rt := &domain.RefreshToken{UserID: user.ID, TokenHash: "hash-1", ExpiresAt: exp}
		if err := repo.CreateRefreshToken(ctx, rt); err != nil {
			t.Fatalf("CreateRefreshToken: %v", err)
		}
		got, err := repo.FindRefreshTokenByHash(ctx, "hash-1")
		if err != nil || got == nil {
			t.Fatalf("FindRefreshTokenByHash: %+v %v", got, err)
		}
		if got.ID != rt.ID || got.UserID != user.ID || got.Revoked || got.ExpiresAt.Sub(exp).Abs() > time.Second {
			t.Fatalf("unexpected token: %+v", got)
		}

		if err := repo.RevokeRefreshToken(ctx, rt.ID); err != nil {
			t.Fatalf("RevokeRefreshToken: %v", err)
		}
		got, _ = repo.FindRefreshTokenByHash(ctx, "hash-1")
		if got == nil || !got.Revoked {
			t.Fatalf("expected revoked token, got %+v", got)
		}
		if err := repo.RevokeRefreshToken(ctx, rt.ID); !errors.Is(err, usecase.ErrInvalidRefresh) {
			t.Fatalf("revoking an already-revoked token: want ErrInvalidRefresh, got %v", err)
		}

		for _, h := range []string{"hash-2", "hash-3"} {
			if err := repo.CreateRefreshToken(ctx, &domain.RefreshToken{UserID: user.ID, TokenHash: h, ExpiresAt: exp}); err != nil {
				t.Fatalf("CreateRefreshToken %s: %v", h, err)
			}
		}
		if err := repo.RevokeAllUserRefreshTokens(ctx, user.ID); err != nil {
			t.Fatalf("RevokeAllUserRefreshTokens: %v", err)
		}
		for _, h := range []string{"hash-2", "hash-3"} {
			got, _ := repo.FindRefreshTokenByHash(ctx, h)
			if got == nil || !got.Revoked {
				t.Fatalf("token %s not revoked: %+v", h, got)
			}
		}
	})
}
