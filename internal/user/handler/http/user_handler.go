package httptransport

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/piresc/gokit/middleware"
	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/user/domain"
)

// ProfileReader captures user-profile reads.
type ProfileReader interface {
	GetProfile(ctx context.Context, userID string) (*domain.User, error)
}

// ProfileWriter captures user-profile writes.
type ProfileWriter interface {
	UpdateProfile(ctx context.Context, userID string, update domain.ProfileUpdate) (*domain.User, error)
}

// UserHandler serves profile endpoints.
type UserHandler struct {
	reader ProfileReader
	writer ProfileWriter
	log    *slog.Logger
}

// NewUserHandler creates a user handler.
func NewUserHandler(reader ProfileReader, writer ProfileWriter, log *slog.Logger) *UserHandler {
	return &UserHandler{reader: reader, writer: writer, log: log}
}

// GetMe handles GET /users/me.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r)
	if !ok {
		response.Error(w, http.StatusUnauthorized, response.CodeUnauthorized, msgAuthRequired)
		return
	}
	user, err := h.reader.GetProfile(r.Context(), claims.Subject)
	if err != nil {
		writeUseCaseError(w, r, h.log, err)
		return
	}
	response.Success(w, http.StatusOK, toProfileResponse(user))
}

// UpdateMe handles PATCH /users/me.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r)
	if !ok {
		response.Error(w, http.StatusUnauthorized, response.CodeUnauthorized, msgAuthRequired)
		return
	}
	var req updateProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidInput, msgInvalidBody)
		return
	}
	user, err := h.writer.UpdateProfile(r.Context(), claims.Subject, domain.ProfileUpdate{DisplayName: req.DisplayName})
	if err != nil {
		writeUseCaseError(w, r, h.log, err)
		return
	}
	response.Success(w, http.StatusOK, toProfileResponse(user))
}
