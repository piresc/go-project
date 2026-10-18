package httptransport

import (
	"context"
	"strings"
	"time"

	"github.com/piresc/go-project/services/user-service/internal/domain"
	"github.com/piresc/go-project/services/user-service/internal/usecase"
)

type fakeAuth struct {
	pair *domain.TokenPair
	err  error

	logoutUser string
	logoutJTI  string
	logoutExp  time.Time
	logoutCall bool
}

func (f *fakeAuth) AuthenticateGoogle(context.Context, string) (*domain.TokenPair, error) {
	return f.pair, f.err
}

func (f *fakeAuth) AuthenticateApple(context.Context, string) (*domain.TokenPair, error) {
	return f.pair, f.err
}

func (f *fakeAuth) RefreshTokens(context.Context, string) (*domain.TokenPair, error) {
	return f.pair, f.err
}

func (f *fakeAuth) Logout(_ context.Context, userID, jti string, exp time.Time) error {
	f.logoutCall = true
	f.logoutUser, f.logoutJTI, f.logoutExp = userID, jti, exp
	return f.err
}

// fakeProfile mimics the use case's display-name validation.
type fakeProfile struct {
	user *domain.User
	err  error
}

func (f *fakeProfile) GetProfile(context.Context, string) (*domain.User, error) {
	return f.user, f.err
}

func (f *fakeProfile) UpdateProfile(_ context.Context, _ string, u domain.ProfileUpdate) (*domain.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	if u.DisplayName == nil || strings.TrimSpace(*u.DisplayName) == "" {
		return nil, usecase.ErrInvalidInput
	}
	out := *f.user
	out.DisplayName = *u.DisplayName
	return &out, nil
}
