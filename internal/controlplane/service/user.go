package service

import (
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/logger"
)

type UserService interface {
	// TODO: Define interface methods
}

type userService struct {
	repos  *repository.Repository
	logger *logger.Logger
}

func NewUserService(repos *repository.Repository, log *logger.Logger) UserService {
	return &userService{repos: repos, logger: log}
}
