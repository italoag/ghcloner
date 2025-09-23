# ghclone

> 🚀 A high-performance, concurrent GitHub repository cloner built with Go

[![CI](https://github.com/italoag/ghcloner/workflows/CI/badge.svg)](https://github.com/italoag/ghcloner/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/italoag/ghcloner)](https://goreportcard.com/report/github.com/italoag/ghcloner)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.24.3+-blue.svg)](https://golang.org)

**ghclone** is a powerful command-line tool designed for efficiently cloning multiple GitHub repositories concurrently. It features an enhanced terminal UI with real-time progress tracking, structured logging, and intelligent worker pool management.

**📖 [Versão em Português](README_pt.md)**

## ✨ Features

- **🔄 Concurrent Processing**: Clone multiple repositories simultaneously using configurable worker pools
- **📊 Real-time Progress Tracking**: Interactive terminal UI with live progress updates
- **🎯 Smart Filtering**: Advanced filtering by language, size, fork status, and update date
- **📁 Flexible Organization**: Support for both GitHub users and organizations
- **🛠️ Multiple Interfaces**: Choose between CLI and TUI (Terminal User Interface)
- **📋 Multiple Output Formats**: Export repository lists as table, JSON, or CSV
- **🔐 Token Support**: GitHub API token integration with rate limiting
- **🏗️ Domain-Driven Design**: Clean architecture with SOLID principles
- **📝 Structured Logging**: Comprehensive logging with configurable levels
- **🐳 Docker Support**: Ready-to-use Docker images

## 🚀 Installation

### 📦 Pre-built Binaries

Download the latest release from the [releases page](https://github.com/italoag/ghcloner/releases):

```bash
# Linux (amd64)
curl -L https://github.com/italoag/ghcloner/releases/latest/download/ghclone-linux-amd64.tar.gz | tar xz
sudo mv ghclone /usr/local/bin/

# macOS (amd64)
curl -L https://github.com/italoag/ghcloner/releases/latest/download/ghclone-darwin-amd64.tar.gz | tar xz
sudo mv ghclone /usr/local/bin/

# Windows (amd64)
# Download ghclone-windows-amd64.zip and extract to your PATH
```

### 🐹 From Source (Go)

```bash
# Install with Go (requires Go 1.24.3+)
go install github.com/italoag/ghcloner/cmd/ghclone@latest

# Or clone and build
git clone https://github.com/italoag/ghcloner.git
cd ghcloner
make build
sudo cp build/ghclone /usr/local/bin/
```

### 🐳 Docker

```bash
# Pull the image
docker pull ghcr.io/italoag/ghclone:latest

# Run with Docker
docker run --rm -v $(pwd):/workspace ghcr.io/italoag/ghclone:latest clone user octocat

# Create an alias for convenience
echo 'alias ghclone="docker run --rm -v $(pwd):/workspace ghcr.io/italoag/ghclone:latest"' >> ~/.bashrc
```

## 📚 Usage

### 🎯 Quick Start

```bash
# Clone all repositories from a user
ghclone clone user octocat

# Clone organization repositories (skip forks)
ghclone clone org microsoft --skip-forks

# List repositories in JSON format
ghclone list user torvalds --format json

# Clone with custom settings
ghclone clone user kubernetes --concurrency 16 --depth 1 --base-dir ./repos
```

### 🔧 Clone Command

Clone repositories from a GitHub user or organization:

```bash
ghclone clone [type] [owner] [flags]
```

**Repository Types:**
- `user` or `users` - Clone from a GitHub user account
- `org` or `orgs` - Clone from a GitHub organization

**Examples:**

```bash
# Basic user cloning
ghclone clone user octocat

# Organization with custom concurrency
ghclone clone org microsoft --concurrency 8

# Include forks and set custom directory
ghclone clone user torvalds --include-forks --base-dir /tmp/repos

# Clone specific branch with shallow depth
ghclone clone org kubernetes --branch main --depth 5

# Clone with debug logging
ghclone clone user facebook --log-level debug
```

**Available Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--base-dir` | Base directory for cloning | `.` |
| `--branch` | Specific branch to clone | default branch |
| `--concurrency` | Number of concurrent workers | `8` |
| `--depth` | Clone depth (0 for full history) | `1` |
| `--include-forks` | Include forked repositories | `false` |
| `--skip-forks` | Skip forked repositories | `true` |
| `--token` | GitHub personal access token | `$GITHUB_TOKEN` |
| `--log-level` | Log level (debug/info/warn/error) | `info` |

### 📋 List Command

List and filter repositories without cloning:

```bash
ghclone list [type] [owner] [flags]
```

**Examples:**

```bash
# List user repositories in table format
ghclone list user octocat

# Export organization repositories as JSON
ghclone list org microsoft --format json

# Filter by language and size
ghclone list user torvalds --language c --min-size 1000000

# Sort by size and limit results
ghclone list org kubernetes --sort size --limit 20

# Filter by update date
ghclone list user facebook --updated-after 2024-01-01

# Export as CSV for spreadsheets
ghclone list org google --format csv --sort updated
```

**Available Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--format` | Output format (table/json/csv) | `table` |
| `--sort` | Sort by (name/size/updated) | `name` |
| `--limit` | Limit number of results | unlimited |
| `--min-size` | Minimum repository size (bytes) | `0` |
| `--max-size` | Maximum repository size (bytes) | unlimited |
| `--language` | Filter by programming language | all |
| `--updated-after` | Filter by update date (YYYY-MM-DD) | all |
| `--include-forks` | Include forked repositories | `false` |
| `--skip-forks` | Skip forked repositories | `true` |

## ⚙️ Configuration

### 🔑 Authentication

ghclone supports GitHub personal access tokens for higher rate limits and private repository access:

```bash
# Set via environment variable
export GITHUB_TOKEN="your_token_here"

# Or pass directly
ghclone clone user octocat --token "your_token_here"
```

**Creating a Token:**
1. Go to GitHub Settings → Developer settings → Personal access tokens
2. Generate a new token with `repo` scope
3. Copy the token and set it as an environment variable

### 🎨 Terminal UI Features

When cloning repositories, ghclone provides a rich terminal interface:

- **📊 Real-time Progress**: Live updates on cloning progress
- **⚡ Throughput Metrics**: Current speed and estimated completion
- **📈 Success/Error Counters**: Track successful and failed operations
- **🎯 Current Operation**: See which repository is being processed
- **📝 Detailed Logging**: Comprehensive logs with configurable levels

### 🗂️ Directory Structure

By default, repositories are cloned with this structure:

```
./
├── repo1/
├── repo2/
└── repo3/
```

You can customize the base directory:

```bash
# Custom base directory
ghclone clone user octocat --base-dir /home/user/projects

# This creates:
/home/user/projects/
├── repo1/
├── repo2/
└── repo3/
```

## 🏗️ Architecture

ghclone is built with clean architecture principles:

```
cmd/ghclone/           # Application entry point
├── main.go

internal/
├── application/       # Business logic layer
│   ├── services/      # Application services
│   └── usecases/      # Use case implementations
├── domain/           # Core business domain
│   ├── cloning/      # Cloning domain
│   ├── repository/   # Repository domain
│   └── shared/       # Shared domain types
├── infrastructure/   # External concerns
│   ├── concurrency/  # Worker pool management
│   ├── git/          # Git operations
│   ├── github/       # GitHub API client
│   └── logging/      # Structured logging
└── interfaces/       # User interfaces
    ├── cli/          # Command-line interface
    └── tui/          # Terminal user interface
```

**Key Design Principles:**
- **Domain-Driven Design**: Clear separation of business logic
- **SOLID Principles**: Single responsibility, dependency inversion
- **Concurrent Processing**: Efficient worker pool implementation
- **Error Handling**: Comprehensive error management
- **Testability**: Clean interfaces for easy testing

## 🛠️ Development

### 📋 Prerequisites

- Go 1.24.3 or later
- Git
- Make (optional, for convenience)

### 🔨 Building

```bash
# Clone the repository
git clone https://github.com/italoag/ghcloner.git
cd ghcloner

# Build for current platform
make build

# Build for all platforms
make build-all

# Build static binary
make build-static
```

### 🧪 Testing

```bash
# Run all tests
make test

# Run tests with coverage
make cover

# Run benchmarks
make bench

# Fast testing during development
make test-fast
```

### 🎯 Quality Checks

```bash
# Run linting
make lint

# Format code
make fmt

# Run security checks
make sec

# Complete quality workflow
make ci
```

### 🐳 Docker Development

```bash
# Build Docker image
make docker-build

# Run with Docker
make docker-run

# Push to registry
make docker-push
```

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

### 📝 Development Workflow

1. **Fork** the repository
2. **Create** a feature branch: `git checkout -b feature/amazing-feature`
3. **Make** your changes following our coding standards
4. **Test** your changes: `make test`
5. **Lint** your code: `make lint`
6. **Commit** your changes: `git commit -m 'Add amazing feature'`
7. **Push** to the branch: `git push origin feature/amazing-feature`
8. **Open** a Pull Request

### 🐛 Bug Reports

When reporting bugs, please include:
- Operating system and version
- Go version
- ghclone version (`ghclone --version`)
- Steps to reproduce
- Expected vs actual behavior
- Any relevant logs or error messages

### 💡 Feature Requests

We'd love to hear your ideas! Please open an issue with:
- Clear description of the feature
- Use case and motivation
- Proposed implementation (if you have ideas)

## 📊 Performance

ghclone is optimized for performance:

- **Concurrent Processing**: Configurable worker pools (default: 8 workers)
- **Memory Efficient**: Streaming operations where possible
- **Rate Limiting**: Respects GitHub API limits
- **Shallow Clones**: Default depth of 1 for faster cloning
- **Progress Tracking**: Minimal overhead real-time updates

**Benchmarks** (approximate, varies by network and system):
- **Single Repository**: 2-5 seconds
- **Organization (50 repos)**: 30-60 seconds with 8 workers
- **Large Organization (200+ repos)**: 2-5 minutes with 16 workers

## 🔍 Troubleshooting

### Common Issues

**Authentication Errors:**
```bash
# Verify your token
curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/user

# Check token scopes
ghclone list user octocat --log-level debug
```

**Rate Limiting:**
```bash
# Use authenticated requests
export GITHUB_TOKEN="your_token_here"

# Reduce concurrency
ghclone clone org large-org --concurrency 4
```

**Network Issues:**
```bash
# Enable debug logging
ghclone clone user octocat --log-level debug

# Check connectivity
curl -I https://api.github.com
```

**Permission Errors:**
```bash
# Ensure directory is writable
ls -la $(pwd)

# Use custom directory
ghclone clone user octocat --base-dir /tmp/repos
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [Charm](https://charm.sh/) for the excellent TUI libraries
- [Cobra](https://cobra.dev/) for the CLI framework
- [Zap](https://github.com/uber-go/zap) for structured logging
- [Ants](https://github.com/panjf2000/ants) for worker pool management

## 📞 Support

- 📧 **Issues**: [GitHub Issues](https://github.com/italoag/ghcloner/issues)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/italoag/ghcloner/discussions)
- 📖 **Documentation**: [Wiki](https://github.com/italoag/ghcloner/wiki)

---

Made with ❤️ by [Italo A. G.](https://github.com/italoag)

**📖 [Read this in Portuguese](README_pt.md)**