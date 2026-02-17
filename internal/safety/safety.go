package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HexSleeves/waggle/internal/config"
)

// Guard enforces safety constraints on worker operations
type Guard struct {
	cfg           config.SafetyConfig
	projectRoot   string
	resolvedPaths []string
}

func NewGuard(cfg config.SafetyConfig, projectRoot string) (*Guard, error) {
	cfg = normalizeSafetyConfig(cfg)

	absRoot, err := canonicalPath(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}

	resolved := make([]string, 0, len(cfg.AllowedPaths))
	for _, p := range cfg.AllowedPaths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(absRoot, p)
		}
		abs, err := canonicalPath(p)
		if err != nil {
			continue
		}
		resolved = append(resolved, abs)
	}
	if len(resolved) == 0 {
		resolved = []string{absRoot}
	}

	return &Guard{
		cfg:           cfg,
		projectRoot:   absRoot,
		resolvedPaths: resolved,
	}, nil
}

// CheckPath verifies a file path is within allowed boundaries.
// Symlinks are resolved to prevent escaping allowed directories.
func (g *Guard) CheckPath(path string) error {
	originalPath := path
	if !filepath.IsAbs(path) {
		path = filepath.Join(g.projectRoot, path)
	}
	resolved, err := canonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	for _, allowed := range g.resolvedPaths {
		if isWithinDir(allowed, resolved) {
			return nil
		}
	}
	return fmt.Errorf("path %q outside allowed directories", originalPath)
}

// CheckCommand verifies a command is not in the blocked list
func (g *Guard) CheckCommand(cmd string) error {
	return g.checkCommandPolicy(cmd)
}

// EnforceCommandBlocking returns whether command blocklist checks should be
// applied for the given adapter.
func (g *Guard) EnforceCommandBlocking(adapterName string) bool {
	name := strings.ToLower(strings.TrimSpace(adapterName))
	for _, allowed := range g.cfg.EnforceOnAdapters {
		if strings.ToLower(strings.TrimSpace(allowed)) == name {
			return true
		}
	}
	return false
}

// CheckFileSize verifies a file doesn't exceed the maximum size
func (g *Guard) CheckFileSize(path string) error {
	if g.cfg.MaxFileSize <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil // File doesn't exist yet, that's OK
	}
	if info.Size() > g.cfg.MaxFileSize {
		return fmt.Errorf("file %q (%d bytes) exceeds max size (%d bytes)", path, info.Size(), g.cfg.MaxFileSize)
	}
	return nil
}

// IsReadOnly returns whether the system is in read-only mode
func (g *Guard) IsReadOnly() bool {
	return g.cfg.ReadOnlyMode
}

// ValidateTaskPaths checks all paths in a task's allowed_paths
func (g *Guard) ValidateTaskPaths(paths []string) error {
	for _, p := range paths {
		if err := g.CheckPath(p); err != nil {
			return err
		}
	}
	return nil
}

// ProjectRoot returns the resolved project root
func (g *Guard) ProjectRoot() string {
	return g.projectRoot
}

func isWithinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := resolvePathWithSymlinks(abs)
	if err != nil {
		// Best effort fallback when symlink resolution is not possible.
		return filepath.Clean(abs), nil
	}
	return filepath.Clean(resolved), nil
}

func resolvePathWithSymlinks(path string) (string, error) {
	// Resolve the deepest existing ancestor and re-append missing suffix parts.
	var suffix []string
	cur := path
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(path), nil
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func normalizeSafetyConfig(cfg config.SafetyConfig) config.SafetyConfig {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch mode {
	case "", config.SafetyModeStrict:
		cfg.Mode = config.SafetyModeStrict
	case config.SafetyModePermissive:
		cfg.Mode = config.SafetyModePermissive
	default:
		cfg.Mode = config.SafetyModeStrict
	}
	if len(cfg.EnforceOnAdapters) == 0 {
		cfg.EnforceOnAdapters = []string{"exec"}
	}
	if len(cfg.BlockedPatterns) == 0 {
		cfg.BlockedPatterns = append([]string(nil), cfg.BlockedCommands...)
	}
	return cfg
}
