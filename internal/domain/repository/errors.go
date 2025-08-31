package repository

import "errors"

// Domain errors for repository operations
var (
	// ErrRepositoryNotFound indicates a repository was not found
	ErrRepositoryNotFound = errors.New("repository not found")

	// ErrInvalidRepositoryName indicates an invalid repository name
	ErrInvalidRepositoryName = errors.New("invalid repository name")

	// ErrInvalidOwner indicates an invalid repository owner
	ErrInvalidOwner = errors.New("invalid repository owner")

	// ErrInvalidCloneURL indicates an invalid clone URL
	ErrInvalidCloneURL = errors.New("invalid clone URL")

	// ErrRepositoryAccessDenied indicates access to repository is denied
	ErrRepositoryAccessDenied = errors.New("repository access denied")

	// ErrRepositoryAlreadyExists indicates a repository already exists locally
	ErrRepositoryAlreadyExists = errors.New("repository already exists")

	// ErrRepositorySizeTooLarge indicates repository is too large
	ErrRepositorySizeTooLarge = errors.New("repository size exceeds limit")
)

// IsNotFoundError checks if an error is a not found error
func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrRepositoryNotFound)
}

// IsValidationError checks if an error is a validation error
func IsValidationError(err error) bool {
	return errors.Is(err, ErrInvalidRepositoryName) ||
		errors.Is(err, ErrInvalidOwner) ||
		errors.Is(err, ErrInvalidCloneURL)
}

// IsAccessError checks if an error is an access error
func IsAccessError(err error) bool {
	return errors.Is(err, ErrRepositoryAccessDenied)
}
