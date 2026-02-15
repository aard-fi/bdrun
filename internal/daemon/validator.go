package daemon

import (
	"fmt"
	"regexp"

	"github.com/aard-fi/bdrun/internal/config"
)

// CommandValidator validates commands against configured rules
type CommandValidator struct {
	rules []*compiledRule
}

// compiledRule represents a compiled command rule
type compiledRule struct {
	cmdPattern  *regexp.Regexp
	allowedArgs []*regexp.Regexp
	deniedArgs  []*regexp.Regexp
	allowPTY    bool
}

// NewCommandValidator creates a new validator from configuration rules
func NewCommandValidator(rules []config.CommandRule) (*CommandValidator, error) {
	compiled := make([]*compiledRule, 0, len(rules))

	for i, rule := range rules {
		cr := &compiledRule{
			allowPTY: rule.AllowPTY,
		}

		// Compile command pattern
		if rule.Regex {
			pattern, err := regexp.Compile(rule.Command)
			if err != nil {
				return nil, fmt.Errorf("invalid command regex in rule %d %q: %w", i, rule.Command, err)
			}
			cr.cmdPattern = pattern
		} else {
			// Exact match - escape and anchor
			pattern := regexp.MustCompile("^" + regexp.QuoteMeta(rule.Command) + "$")
			cr.cmdPattern = pattern
		}

		// Compile allowed arg patterns
		for j, allowed := range rule.AllowedArgs {
			pattern, err := regexp.Compile(allowed)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed arg regex in rule %d, arg %d %q: %w", i, j, allowed, err)
			}
			cr.allowedArgs = append(cr.allowedArgs, pattern)
		}

		// Compile denied arg patterns
		for j, denied := range rule.DeniedArgs {
			pattern, err := regexp.Compile(denied)
			if err != nil {
				return nil, fmt.Errorf("invalid denied arg regex in rule %d, arg %d %q: %w", i, j, denied, err)
			}
			cr.deniedArgs = append(cr.deniedArgs, pattern)
		}

		compiled = append(compiled, cr)
	}

	return &CommandValidator{rules: compiled}, nil
}

// Validate validates a command and its arguments
func (cv *CommandValidator) Validate(cmd string, args []string, needsPTY bool) error {
	// Find matching rule
	var matchedRule *compiledRule
	for _, rule := range cv.rules {
		if rule.cmdPattern.MatchString(cmd) {
			matchedRule = rule
			break
		}
	}

	if matchedRule == nil {
		return fmt.Errorf("command not allowed: %s", cmd)
	}

	// Check PTY permission
	if needsPTY && !matchedRule.allowPTY {
		return fmt.Errorf("PTY not allowed for command: %s", cmd)
	}

	// Validate each argument
	for argIdx, arg := range args {
		// Check denied patterns first (security-critical)
		for _, denied := range matchedRule.deniedArgs {
			if denied.MatchString(arg) {
				return fmt.Errorf("argument %d explicitly denied: %s", argIdx, arg)
			}
		}

		// Check allowed patterns
		allowed := false
		for _, allowedPattern := range matchedRule.allowedArgs {
			if allowedPattern.MatchString(arg) {
				allowed = true
				break
			}
		}

		if !allowed && len(matchedRule.allowedArgs) > 0 {
			return fmt.Errorf("argument %d not allowed: %s", argIdx, arg)
		}
	}

	return nil
}

// ValidateEnv validates environment variables against allowed list
func ValidateEnv(env map[string]string, allowed []string) error {
	if len(allowed) == 0 {
		// No env vars allowed
		if len(env) > 0 {
			return fmt.Errorf("environment variables not allowed")
		}
		return nil
	}

	// Compile allowed patterns
	allowedPatterns := make([]*regexp.Regexp, 0, len(allowed))
	for _, pattern := range allowed {
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return fmt.Errorf("invalid env var pattern %q: %w", pattern, err)
		}
		allowedPatterns = append(allowedPatterns, re)
	}

	// Check each env var
	for key := range env {
		allowed := false
		for _, pattern := range allowedPatterns {
			if pattern.MatchString(key) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("environment variable not allowed: %s", key)
		}
	}

	return nil
}
