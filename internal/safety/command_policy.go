package safety

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/HexSleeves/waggle/internal/config"
	"mvdan.cc/sh/v3/syntax"
)

type commandInvocation struct {
	Name        string
	Args        []string
	NameDynamic bool
}

func (g *Guard) checkCommandPolicy(cmd string) error {
	script := strings.TrimSpace(cmd)
	if script == "" {
		return nil
	}

	invocations, err := parseCommandInvocations(script)
	if err != nil {
		if g.cfg.Mode == config.SafetyModeStrict {
			return fmt.Errorf("command parse failed in strict mode: %w", err)
		}
		return nil
	}

	blockedExecs := toLowerSet(g.cfg.BlockedExecutables)
	allowExecs := toLowerSet(g.cfg.AllowExecutables)
	blockedRules := buildBlockedRules(g.cfg)

	for _, inv := range invocations {
		if len(inv.Args) == 0 {
			continue
		}

		name := strings.ToLower(inv.Name)
		if name == "" || inv.NameDynamic {
			if g.cfg.Mode == config.SafetyModeStrict {
				return fmt.Errorf("dynamic command name is not allowed in strict mode")
			}
			continue
		}

		if _, ok := allowExecs[name]; ok {
			continue
		}

		if isIndirectExecution(inv) {
			if g.cfg.Mode == config.SafetyModeStrict {
				return fmt.Errorf("indirect command execution is blocked in strict mode: %q", name)
			}
			continue
		}

		if _, blocked := blockedExecs[name]; blocked {
			if g.cfg.Mode == config.SafetyModePermissive && !isHighConfidenceInvocation(inv.Args) {
				continue
			}
			return fmt.Errorf("command uses blocked executable: %q", name)
		}

		for _, rule := range blockedRules {
			if !matchesRule(inv.Args, rule) {
				continue
			}
			if g.cfg.Mode == config.SafetyModePermissive && !isHighConfidenceInvocation(inv.Args) && !isHighConfidenceRule(rule) {
				continue
			}
			return fmt.Errorf("command matches blocked pattern: %q", strings.Join(rule, " "))
		}
	}

	return nil
}

func parseCommandInvocations(script string) ([]commandInvocation, error) {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, err
	}

	invocations := make([]commandInvocation, 0)
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		args := make([]string, 0, len(call.Args))
		nameDynamic := false
		for i, w := range call.Args {
			token, dynamic, tokenErr := wordToToken(w)
			if tokenErr != nil {
				dynamic = true
				token = ""
			}
			if i == 0 && dynamic {
				nameDynamic = true
			}
			args = append(args, strings.ToLower(strings.TrimSpace(token)))
		}

		inv := commandInvocation{
			Name:        args[0],
			Args:        args,
			NameDynamic: nameDynamic,
		}
		invocations = append(invocations, inv)
		return true
	})

	return invocations, nil
}

func wordToToken(w *syntax.Word) (string, bool, error) {
	dynamic := false
	syntax.Walk(w, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			dynamic = true
		}
		return true
	})

	var buf bytes.Buffer
	printer := syntax.NewPrinter()
	if err := printer.Print(&buf, w); err != nil {
		return "", dynamic, err
	}
	return buf.String(), dynamic, nil
}

func buildBlockedRules(cfg config.SafetyConfig) [][]string {
	patterns := make([]string, 0, len(cfg.BlockedPatterns)+len(cfg.BlockedCommands))
	patterns = append(patterns, cfg.BlockedPatterns...)
	patterns = append(patterns, cfg.BlockedCommands...)

	rules := make([][]string, 0, len(patterns))
	for _, p := range patterns {
		tokens := parseRuleTokens(p)
		if len(tokens) == 0 {
			continue
		}
		rules = append(rules, tokens)
	}
	return rules
}

func parseRuleTokens(pattern string) []string {
	invocations, err := parseCommandInvocations(pattern)
	if err == nil && len(invocations) > 0 {
		return invocations[0].Args
	}

	parts := strings.Fields(strings.ToLower(strings.TrimSpace(pattern)))
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func matchesRule(args, rule []string) bool {
	if len(rule) == 0 || len(args) < len(rule) {
		return false
	}
	for i := 0; i < len(rule); i++ {
		if args[i] != rule[i] {
			return false
		}
	}
	return true
}

func toLowerSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}
	return set
}

func isIndirectExecution(inv commandInvocation) bool {
	if len(inv.Args) == 0 {
		return false
	}
	name := inv.Args[0]
	if name == "eval" || name == "." || name == "source" {
		return true
	}
	if (name == "sh" || name == "bash" || name == "zsh" || name == "ksh") && len(inv.Args) >= 2 {
		return inv.Args[1] == "-c"
	}
	return false
}

func isHighConfidenceRule(rule []string) bool {
	return isHighConfidenceInvocation(rule)
}

func isHighConfidenceInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}

	name := args[0]
	switch {
	case name == "rm":
		return hasFlag(args, "-rf", "-fr", "--no-preserve-root") && hasArg(args, "/")
	case strings.HasPrefix(name, "mkfs"):
		return true
	case name == "dd":
		return hasPrefixArg(args, "if=/dev/zero")
	case name == "sudo" && len(args) > 1:
		return isHighConfidenceInvocation(args[1:])
	}
	return false
}

func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func hasArg(args []string, value string) bool {
	for _, a := range args {
		if a == value {
			return true
		}
	}
	return false
}

func hasPrefixArg(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
