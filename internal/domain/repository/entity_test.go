package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_NewRepository(t *testing.T) {
	tests := []struct {
		name          string
		id            RepositoryID
		repoName      string
		cloneURL      string
		owner         string
		isFork        bool
		size          int64
		defaultBranch string
		wantErr       bool
	}{
		{
			name:          "valid repository",
			id:            12345,
			repoName:      "test-repo",
			cloneURL:      "https://github.com/owner/test-repo.git",
			owner:         "owner",
			isFork:        false,
			size:          1024,
			defaultBranch: "main",
			wantErr:       false,
		},
		{
			name:          "empty name",
			id:            12345,
			repoName:      "",
			cloneURL:      "https://github.com/owner/test-repo.git",
			owner:         "owner",
			isFork:        false,
			size:          1024,
			defaultBranch: "main",
			wantErr:       true,
		},
		{
			name:          "empty owner",
			id:            12345,
			repoName:      "test-repo",
			cloneURL:      "https://github.com/owner/test-repo.git",
			owner:         "",
			isFork:        false,
			size:          1024,
			defaultBranch: "main",
			wantErr:       true,
		},
		{
			name:          "invalid clone URL",
			id:            12345,
			repoName:      "test-repo",
			cloneURL:      "not-a-url",
			owner:         "owner",
			isFork:        false,
			size:          1024,
			defaultBranch: "main",
			wantErr:       true,
		},
		{
			name:          "negative size",
			id:            12345,
			repoName:      "test-repo",
			cloneURL:      "https://github.com/owner/test-repo.git",
			owner:         "owner",
			isFork:        false,
			size:          -1,
			defaultBranch: "main",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewRepository(
				tt.id,
				tt.repoName,
				tt.cloneURL,
				tt.owner,
				tt.isFork,
				tt.size,
				tt.defaultBranch,
			)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, repo)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, repo)
				assert.Equal(t, tt.id, repo.ID)
				assert.Equal(t, tt.repoName, repo.Name)
				assert.Equal(t, tt.cloneURL, repo.CloneURL)
				assert.Equal(t, tt.owner, repo.Owner)
				assert.Equal(t, tt.isFork, repo.IsFork)
				assert.Equal(t, tt.size, repo.Size)
				assert.Equal(t, tt.defaultBranch, repo.DefaultBranch)
				assert.WithinDuration(t, time.Now(), repo.UpdatedAt, time.Second)
			}
		})
	}
}

func TestRepository_ValidateCloneURL(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		wantErr  bool
	}{
		{
			name:     "valid HTTPS URL",
			cloneURL: "https://github.com/owner/repo.git",
			wantErr:  false,
		},
		{
			name:     "valid SSH URL",
			cloneURL: "ssh://git@github.com/owner/repo.git",
			wantErr:  false,
		},
		{
			name:     "empty URL",
			cloneURL: "",
			wantErr:  true,
		},
		{
			name:     "invalid URL format",
			cloneURL: "not-a-url",
			wantErr:  true,
		},
		{
			name:     "unsupported protocol",
			cloneURL: "ftp://github.com/owner/repo.git",
			wantErr:  true,
		},
		{
			name:     "missing host",
			cloneURL: "https:///owner/repo.git",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &Repository{
				ID:       1,
				Name:     "test",
				CloneURL: tt.cloneURL,
				Owner:    "owner",
			}

			err := repo.ValidateCloneURL()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_GetLocalPath(t *testing.T) {
	repo := &Repository{
		Name: "test-repo",
	}

	tests := []struct {
		name    string
		baseDir string
		want    string
	}{
		{
			name:    "simple path",
			baseDir: "/tmp",
			want:    "/tmp/test-repo",
		},
		{
			name:    "path with trailing slash",
			baseDir: "/tmp/",
			want:    "/tmp/test-repo",
		},
		{
			name:    "nested path",
			baseDir: "/home/user/projects",
			want:    "/home/user/projects/test-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repo.GetLocalPath(tt.baseDir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepository_GetFullName(t *testing.T) {
	repo := &Repository{
		Name:  "test-repo",
		Owner: "test-owner",
	}

	assert.Equal(t, "test-owner/test-repo", repo.GetFullName())
}

func TestRepository_IsPublic(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		want     bool
	}{
		{
			name:     "HTTPS URL is public",
			cloneURL: "https://github.com/owner/repo.git",
			want:     true,
		},
		{
			name:     "SSH URL is not public",
			cloneURL: "git@github.com:owner/repo.git",
			want:     false,
		},
		{
			name:     "SSH with protocol is not public",
			cloneURL: "ssh://git@github.com/owner/repo.git",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &Repository{
				CloneURL: tt.cloneURL,
			}
			assert.Equal(t, tt.want, repo.IsPublic())
		})
	}
}

func TestRepository_Equal(t *testing.T) {
	repo1 := &Repository{
		ID:       123,
		CloneURL: "https://github.com/owner/repo.git",
	}

	repo2 := &Repository{
		ID:       123,
		CloneURL: "https://github.com/owner/repo.git",
	}

	repo3 := &Repository{
		ID:       456,
		CloneURL: "https://github.com/owner/repo.git",
	}

	repo4 := &Repository{
		ID:       123,
		CloneURL: "https://github.com/owner/other-repo.git",
	}

	assert.True(t, repo1.Equal(repo2))
	assert.False(t, repo1.Equal(repo3))
	assert.False(t, repo1.Equal(repo4))
	assert.False(t, repo1.Equal(nil))
}

func TestRepository_String(t *testing.T) {
	repo := &Repository{
		Name:  "test-repo",
		Owner: "test-owner",
	}

	assert.Equal(t, "test-owner/test-repo", repo.String())
}

func TestRepository_Validate(t *testing.T) {
	tests := []struct {
		name string
		repo *Repository
		want bool
	}{
		{
			name: "valid repository",
			repo: &Repository{
				ID:       123,
				Name:     "test-repo",
				CloneURL: "https://github.com/owner/repo.git",
				Owner:    "owner",
				Size:     1024,
			},
			want: true,
		},
		{
			name: "empty name",
			repo: &Repository{
				ID:       123,
				Name:     "",
				CloneURL: "https://github.com/owner/repo.git",
				Owner:    "owner",
				Size:     1024,
			},
			want: false,
		},
		{
			name: "empty owner",
			repo: &Repository{
				ID:       123,
				Name:     "test-repo",
				CloneURL: "https://github.com/owner/repo.git",
				Owner:    "",
				Size:     1024,
			},
			want: false,
		},
		{
			name: "invalid URL",
			repo: &Repository{
				ID:       123,
				Name:     "test-repo",
				CloneURL: "invalid-url",
				Owner:    "owner",
				Size:     1024,
			},
			want: false,
		},
		{
			name: "negative size",
			repo: &Repository{
				ID:       123,
				Name:     "test-repo",
				CloneURL: "https://github.com/owner/repo.git",
				Owner:    "owner",
				Size:     -1,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.repo.Validate()
			if tt.want {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
