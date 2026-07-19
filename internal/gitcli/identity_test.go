package gitcli

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeRemote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remote     string
		normalized string
		projectID  string
		repository string
	}{
		{
			name:       "HTTPS",
			remote:     "  https://GitHub.COM/hossainemruz/taskctl.git  ",
			normalized: "github.com/hossainemruz/taskctl",
			projectID:  "hossainemruz_taskctl",
			repository: "taskctl",
		},
		{
			name:       "SCP SSH",
			remote:     "git@github.com:hossainemruz/taskctl.git",
			normalized: "github.com/hossainemruz/taskctl",
			projectID:  "hossainemruz_taskctl",
			repository: "taskctl",
		},
		{
			name:       "SSH URL",
			remote:     "ssh://git@GitLab.com/org/team/project.git/",
			normalized: "gitlab.com/org/team/project",
			projectID:  "org_team_project",
			repository: "project",
		},
		{
			name:       "SSH port",
			remote:     "ssh://git@example.com:2222/org/project.git",
			normalized: "example.com:2222/org/project",
			projectID:  "org_project",
			repository: "project",
		},
		{
			name:       "credentials are removed",
			remote:     "https://token:secret@github.com/org/repo.git",
			normalized: "github.com/org/repo",
			projectID:  "org_repo",
			repository: "repo",
		},
		{
			name:       "sanitized segments",
			remote:     "git@example.com:my-org/team.name/repo_name.git",
			normalized: "example.com/my-org/team.name/repo_name",
			projectID:  "my_org_team_name_repo_name",
			repository: "repo_name",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeRemote(test.remote)
			if err != nil {
				t.Fatalf("NormalizeRemote() error = %v", err)
			}
			if got.Normalized != test.normalized || got.ProjectID != test.projectID || got.RepositoryName != test.repository {
				t.Fatalf("NormalizeRemote() = %#v", got)
			}
		})
	}
}

func TestNormalizeRemoteRejectsUnsupportedAndMalformedValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		remote string
		want   error
	}{
		{remote: "/tmp/repo", want: ErrUnsupportedRemote},
		{remote: "../repo", want: ErrUnsupportedRemote},
		{remote: "C:/Users/person/repo", want: ErrUnsupportedRemote},
		{remote: "file:///tmp/repo", want: ErrUnsupportedRemote},
		{remote: "git://example.com/org/repo", want: ErrUnsupportedRemote},
		{remote: "https://example.com/repo", want: ErrInvalidRemote},
		{remote: "https://example.com/org/../repo", want: ErrInvalidRemote},
		{remote: "https://example.com/org/repo.git?token=secret", want: ErrInvalidRemote},
		{remote: "https://example.com/org%2Frepo.git", want: ErrInvalidRemote},
		{remote: "git@:org/repo.git", want: ErrInvalidRemote},
	}
	for _, test := range tests {
		_, err := NormalizeRemote(test.remote)
		if !errors.Is(err, test.want) {
			t.Errorf("NormalizeRemote(%q) error = %v, want %v", test.remote, err, test.want)
		}
		if strings.Contains(err.Error(), "token=secret") {
			t.Errorf("NormalizeRemote() exposed credential-bearing remote: %v", err)
		}
	}
}

func TestNormalizeRepository(t *testing.T) {
	t.Parallel()
	got, err := NormalizeRepository(" GitHub.COM/org/team/repo.git ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Normalized != "github.com/org/team/repo" || got.ProjectID != "org_team_repo" {
		t.Fatalf("NormalizeRepository() = %#v", got)
	}
	for _, invalid := range []string{"org/repo", "https://example.com/org/repo", "user@example.com/org/repo"} {
		if _, err := NormalizeRepository(invalid); !errors.Is(err, ErrInvalidRemote) {
			t.Errorf("NormalizeRepository(%q) error = %v", invalid, err)
		}
	}
}
