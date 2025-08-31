# Gemini Code Assistant Context

## Project Overview

This project is a Go application named `ghclone`, designed for concurrently cloning GitHub repositories. It follows a clean, Domain-Driven Design (DDD) architecture, separating concerns into `domain`, `application`, `infrastructure`, and `interfaces` layers.

The application is a command-line interface (CLI) tool built with `cobra` and `fang` for a modern, user-friendly experience. It leverages a worker pool pattern using the `ants` library for efficient concurrent cloning of multiple repositories.

**Key Features:**

*   Concurrent cloning of GitHub repositories.
*   Support for both user and organization repositories.
*   TUI for real-time progress tracking.
*   Structured logging with `zap`.
*   GitHub API rate limiting.
*   Filtering of repositories (e.g., skip forks).

**Key Technologies:**

*   **Language:** Go
*   **CLI Framework:** `cobra`, `fang`
*   **Concurrency:** `ants` worker pool
*   **Logging:** `zap` (structured logging)
*   **Testing:** `testify`
*   **UI:** `bubbletea` for terminal user interfaces

## Architecture

The project follows a clean architecture with the following structure:

*   `cmd/ghclone`: Application entry point.
*   `internal/domain`: Contains the core business logic, entities, and domain services.
*   `internal/application`: Implements the application's use cases and services.
*   `internal/infrastructure`: Handles external concerns like Git operations, GitHub API interaction, logging, and concurrency.
*   `internal/interfaces`: Defines the user interfaces, including the CLI.

## Core Components

### Application Use Cases

*   **`FetchRepositoriesUseCase`**: Fetches a list of repositories from GitHub for a given user or organization. It supports filtering and pagination.
*   **`CloneRepositoriesUseCase`**: Manages the concurrent cloning of a list of repositories. It creates clone jobs, submits them to a worker pool, and tracks their progress.
*   **`ValidateOwnerExistsUseCase`**: Checks if a GitHub user or organization exists.
*   **`CloneSingleRepositoryUseCase`**: Handles the cloning of a single repository.

### Infrastructure

*   **`github.Client`**: A client for interacting with the GitHub API. It handles authentication, rate limiting, and fetching repository data.
*   **`git.Client`**: A client for executing Git commands. It's used to clone repositories.
*   **`concurrency.WorkerPool`**: A worker pool for managing concurrent tasks. It's used to clone multiple repositories in parallel.
*   **`logging.Logger`**: A structured logger using `zap`.

### Domain

*   **`repository.Repository`**: An entity representing a GitHub repository. It includes fields for its ID, name, clone URL, owner, and other metadata. It also has methods for validation and for getting the local path and full name.
*   **`cloning.CloneJob`**: Represents a single repository cloning task. It includes the repository to be cloned, the destination directory, cloning options, and the job's status. It also has methods for managing the job's lifecycle, such as marking it as started, completed, failed, or skipped.
*   **`cloning.Progress`**: Tracks the progress of the cloning process, including the number of completed, failed, and skipped jobs, as well as the estimated time remaining. The `ProgressTracker` entity is used to manage and update the progress state.

## Building and Running

The project uses a `Makefile` for common development tasks.

**Build the application:**

```bash
make build
```

This will create an executable at `build/ghclone`.

**Run the application:**

```bash
# Clone all repositories from a user
./build/ghclone clone user <username>

# Clone all repositories from an organization
./build/ghclone clone org <orgname>
```

**Run tests:**

```bash
# Run all tests
make test

# Run unit tests
make test-unit

# Run integration tests
make test-integration
```

**Run linter:**

```bash
make lint
```

## Development Conventions

*   **Code Style:** The project follows standard Go formatting, enforced by `go fmt`.
*   **Testing:** The project has a comprehensive test suite, including unit and integration tests. The `testify` library is used for assertions.
*   **Dependency Management:** Go modules are used for dependency management.
*   **CI/CD:** The project has GitHub Actions workflows for CI/CD, defined in the `.github/workflows` directory.
