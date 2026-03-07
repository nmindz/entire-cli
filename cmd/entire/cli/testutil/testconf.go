package testutil

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing/object"
)

type testConfig struct {
	name  string
	email string
}

var (
	cfg     testConfig
	cfgOnce sync.Once
)

const (
	defaultGitTestName  = "Test User"
	defaultGitTestEmail = "test@example.com"
)

func loadConfig() {
	cfgOnce.Do(func() {
		cfg = testConfig{
			name:  defaultGitTestName,
			email: defaultGitTestEmail,
		}

		root := findTestConfRoot()
		if root == "" {
			return
		}

		// Try tests.conf first, then tests.conf.example
		for _, name := range []string{"tests.conf", "tests.conf.example"} {
			path := filepath.Join(root, name)
			parsed, ok := parseConfFile(path)
			if !ok {
				continue
			}
			if v, exists := parsed["GIT_TEST_USER_NAME"]; exists && v != "" {
				cfg.name = v
			}
			if v, exists := parsed["GIT_TEST_USER_EMAIL"]; exists && v != "" {
				cfg.email = v
			}
			break
		}
	})
}

func parseConfFile(path string) (map[string]string, bool) {
	//nolint:gosec // test config file path is derived from runtime.Caller, not user input
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result, true
}

// findTestConfRoot walks up from this source file to find go.mod.
func findTestConfRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// GitName returns the configured test git user name.
func GitName() string {
	loadConfig()
	return cfg.name
}

// GitEmail returns the configured test git user email.
func GitEmail() string {
	loadConfig()
	return cfg.email
}

// TestAuthor returns an object.Signature with the configured test identity and current time.
func TestAuthor() *object.Signature {
	return &object.Signature{
		Name:  GitName(),
		Email: GitEmail(),
		When:  time.Now(),
	}
}
