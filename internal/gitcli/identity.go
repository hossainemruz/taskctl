// Package gitcli derives repository facts through the installed Git CLI.
package gitcli

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
)

var (
	ErrInvalidRemote     = errors.New("invalid Git remote")
	ErrUnsupportedRemote = errors.New("unsupported Git remote")
	ErrNoOrigin          = errors.New("Git origin remote not configured")
	ErrDetachedHEAD      = errors.New("Git HEAD is detached")
	ErrCommand           = errors.New("Git command failed")
)

// Repository identifies a project independently of checkout location and URL
// transport. Normalized includes the host to disambiguate providers; ProjectID
// is the concise, path-safe vault directory name.
type Repository struct {
	Normalized     string
	ProjectID      string
	RepositoryName string
}

// NormalizeRemote accepts the portable URL forms supported by the MVP and
// converts them to host/ownership/repository. Local paths and file URLs are
// deliberately rejected because they are not cross-device identities.
func NormalizeRemote(remote string) (Repository, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" || strings.ContainsAny(remote, "\x00\r\n") {
		return Repository{}, fmt.Errorf("%w: remote is empty or contains control characters", ErrInvalidRemote)
	}
	if strings.Contains(remote, "\\") || strings.HasPrefix(remote, "/") ||
		strings.HasPrefix(remote, "./") || strings.HasPrefix(remote, "../") ||
		strings.HasPrefix(remote, "~") || isDrivePath(remote) {
		return Repository{}, fmt.Errorf("%w: local filesystem remotes are not portable", ErrUnsupportedRemote)
	}

	if separator := strings.Index(remote, "://"); separator >= 0 {
		return normalizeURLRemote(remote)
	}
	return normalizeSCPRemote(remote)
}

// NormalizeRepository validates an explicit normalized repository identity.
// It accepts host/path rather than a transport URL so explicit registration can
// work in repositories without a usable origin.
func NormalizeRepository(value string) (Repository, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, "@\\\x00\r\n?#") {
		return Repository{}, fmt.Errorf("%w: repository identity must be host/path", ErrInvalidRemote)
	}
	separator := strings.IndexByte(value, '/')
	if separator <= 0 || separator == len(value)-1 {
		return Repository{}, fmt.Errorf("%w: repository identity must contain host and path", ErrInvalidRemote)
	}
	host, err := normalizeHost(value[:separator])
	if err != nil {
		return Repository{}, err
	}
	return buildRepository(host, value[separator+1:])
}

func normalizeURLRemote(remote string) (Repository, error) {
	parsed, err := url.Parse(remote)
	if err != nil {
		return Repository{}, fmt.Errorf("%w: malformed remote URL", ErrInvalidRemote)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh":
	case "file":
		return Repository{}, fmt.Errorf("%w: file remotes are not portable", ErrUnsupportedRemote)
	default:
		return Repository{}, fmt.Errorf("%w: remote URL scheme is not supported", ErrUnsupportedRemote)
	}
	if parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Repository{}, fmt.Errorf("%w: remote URL must contain only a host and path", ErrInvalidRemote)
	}
	if containsEscapedSeparator(parsed.EscapedPath()) {
		return Repository{}, fmt.Errorf("%w: escaped path separators are ambiguous", ErrInvalidRemote)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return Repository{}, fmt.Errorf("%w: remote URL host is empty", ErrInvalidRemote)
	}
	host, err := normalizeHost(hostname)
	if err != nil {
		return Repository{}, err
	}
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return buildRepository(host, parsed.Path)
}

func normalizeSCPRemote(remote string) (Repository, error) {
	separator := strings.IndexByte(remote, ':')
	if separator <= 0 || separator == len(remote)-1 {
		return Repository{}, fmt.Errorf("%w: remote must be HTTPS, SSH URL, or SCP-style SSH", ErrUnsupportedRemote)
	}
	left := remote[:separator]
	path := remote[separator+1:]
	if strings.Contains(path, ":") {
		return Repository{}, fmt.Errorf("%w: malformed SCP-style remote", ErrInvalidRemote)
	}
	if at := strings.LastIndexByte(left, '@'); at >= 0 {
		if at == 0 || at == len(left)-1 {
			return Repository{}, fmt.Errorf("%w: malformed SCP-style user or host", ErrInvalidRemote)
		}
		left = left[at+1:]
	}
	host, err := normalizeHost(left)
	if err != nil {
		return Repository{}, err
	}
	return buildRepository(host, path)
}

func normalizeHost(host string) (string, error) {
	if host == "" || host != strings.TrimSpace(host) || strings.ContainsAny(host, "/\\@\x00\r\n") {
		return "", fmt.Errorf("%w: remote host is invalid", ErrInvalidRemote)
	}
	for _, character := range host {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", fmt.Errorf("%w: remote host is invalid", ErrInvalidRemote)
		}
	}
	return strings.ToLower(host), nil
}

func buildRepository(host, remotePath string) (Repository, error) {
	remotePath = strings.Trim(remotePath, "/")
	parts := strings.Split(remotePath, "/")
	if len(parts) < 2 {
		return Repository{}, fmt.Errorf("%w: remote path must contain ownership and repository", ErrInvalidRemote)
	}
	for index, part := range parts {
		if part == "" || part == "." || part == ".." || part != strings.TrimSpace(part) ||
			strings.ContainsAny(part, "\\\x00\r\n?#") {
			return Repository{}, fmt.Errorf("%w: remote path contains an invalid segment", ErrInvalidRemote)
		}
		if index == len(parts)-1 && strings.HasSuffix(strings.ToLower(part), ".git") {
			parts[index] = part[:len(part)-len(".git")]
			if parts[index] == "" {
				return Repository{}, fmt.Errorf("%w: repository name is empty", ErrInvalidRemote)
			}
		}
	}

	projectParts := make([]string, len(parts))
	for index, part := range parts {
		projectParts[index] = sanitizeProjectPart(part)
		if projectParts[index] == "" {
			return Repository{}, fmt.Errorf("%w: remote path cannot produce a portable project ID", ErrInvalidRemote)
		}
	}
	return Repository{
		Normalized:     host + "/" + strings.Join(parts, "/"),
		ProjectID:      strings.Join(projectParts, "_"),
		RepositoryName: parts[len(parts)-1],
	}, nil
}

func sanitizeProjectPart(value string) string {
	var result strings.Builder
	lastUnderscore := false
	for _, character := range value {
		valid := (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9')
		if valid {
			result.WriteRune(character)
			lastUnderscore = false
		} else if !lastUnderscore && result.Len() > 0 {
			result.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(result.String(), "_")
}

func containsEscapedSeparator(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c")
}

func isDrivePath(value string) bool {
	return len(value) >= 3 &&
		((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) &&
		value[1] == ':' && value[2] == '/'
}
