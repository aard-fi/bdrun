package daemon

import (
	"testing"

	"github.com/aard-fi/bdrun/internal/config"
)

func TestCommandValidator_ExactMatch(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command:     "podman",
			Regex:       false,
			AllowedArgs: []string{".*"},
			AllowPTY:    true,
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		cmd       string
		args      []string
		needsPTY  bool
		shouldErr bool
	}{
		{
			name:      "exact match allowed",
			cmd:       "podman",
			args:      []string{"ps"},
			needsPTY:  false,
			shouldErr: false,
		},
		{
			name:      "exact match not found",
			cmd:       "docker",
			args:      []string{"ps"},
			needsPTY:  false,
			shouldErr: true,
		},
		{
			name:      "PTY allowed",
			cmd:       "podman",
			args:      []string{"exec", "-it"},
			needsPTY:  true,
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cmd, tt.args, tt.needsPTY)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_RegexMatch(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command:     "pod.*",
			Regex:       true,
			AllowedArgs: []string{".*"},
			AllowPTY:    true,
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		cmd       string
		shouldErr bool
	}{
		{
			name:      "regex match podman",
			cmd:       "podman",
			shouldErr: false,
		},
		{
			name:      "regex match podlet",
			cmd:       "podlet",
			shouldErr: false,
		},
		{
			name:      "regex no match docker",
			cmd:       "docker",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cmd, []string{}, false)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_AllowedArgs(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command: "podman",
			Regex:   false,
			AllowedArgs: []string{
				"^(run|ps|images|exec)$", // Only these subcommands allowed
			},
			AllowPTY: true,
		},
		{
			Command: "docker",
			Regex:   false,
			AllowedArgs: []string{
				"^(run|ps|images|exec)$", // First arg must be subcommand
				".*",                      // Other args allowed
			},
			AllowPTY: true,
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		cmd       string
		args      []string
		shouldErr bool
	}{
		{
			name:      "podman allowed subcommand ps",
			cmd:       "podman",
			args:      []string{"ps"},
			shouldErr: false,
		},
		{
			name:      "podman disallowed subcommand build",
			cmd:       "podman",
			args:      []string{"build"},
			shouldErr: true,
		},
		{
			name:      "podman disallowed with extra args",
			cmd:       "podman",
			args:      []string{"ps", "-a"},
			shouldErr: true,
		},
		{
			name:      "docker allowed subcommand run with args",
			cmd:       "docker",
			args:      []string{"run", "-it", "alpine"},
			shouldErr: false,
		},
		{
			name:      "docker allowed exec with args",
			cmd:       "docker",
			args:      []string{"exec", "-it", "container", "sh"},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cmd, tt.args, false)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_DeniedArgs(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command:     "podman",
			Regex:       false,
			AllowedArgs: []string{".*"},
			DeniedArgs: []string{
				"--privileged",
				"--cap-add",
			},
			AllowPTY: true,
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		args      []string
		shouldErr bool
	}{
		{
			name:      "safe args",
			args:      []string{"run", "alpine"},
			shouldErr: false,
		},
		{
			name:      "denied --privileged",
			args:      []string{"run", "--privileged", "alpine"},
			shouldErr: true,
		},
		{
			name:      "denied --cap-add",
			args:      []string{"run", "--cap-add", "SYS_ADMIN", "alpine"},
			shouldErr: true,
		},
		{
			name:      "safe with similar name",
			args:      []string{"run", "--publish", "8080:80", "nginx"},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate("podman", tt.args, false)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none for args: %v", tt.args)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_PTYPermission(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command:     "echo",
			Regex:       false,
			AllowedArgs: []string{".*"},
			AllowPTY:    false, // PTY not allowed
		},
		{
			Command:     "bash",
			Regex:       false,
			AllowedArgs: []string{".*"},
			AllowPTY:    true, // PTY allowed
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		cmd       string
		needsPTY  bool
		shouldErr bool
	}{
		{
			name:      "echo without PTY",
			cmd:       "echo",
			needsPTY:  false,
			shouldErr: false,
		},
		{
			name:      "echo with PTY denied",
			cmd:       "echo",
			needsPTY:  true,
			shouldErr: true,
		},
		{
			name:      "bash without PTY",
			cmd:       "bash",
			needsPTY:  false,
			shouldErr: false,
		},
		{
			name:      "bash with PTY allowed",
			cmd:       "bash",
			needsPTY:  true,
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cmd, []string{}, tt.needsPTY)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_InvalidRegex(t *testing.T) {
	tests := []struct {
		name      string
		rules     []config.CommandRule
		shouldErr bool
	}{
		{
			name: "invalid command regex",
			rules: []config.CommandRule{
				{
					Command:     "[invalid",
					Regex:       true,
					AllowedArgs: []string{".*"},
				},
			},
			shouldErr: true,
		},
		{
			name: "invalid allowed arg regex",
			rules: []config.CommandRule{
				{
					Command:     "test",
					Regex:       false,
					AllowedArgs: []string{"[invalid"},
				},
			},
			shouldErr: true,
		},
		{
			name: "invalid denied arg regex",
			rules: []config.CommandRule{
				{
					Command:     "test",
					Regex:       false,
					AllowedArgs: []string{".*"},
					DeniedArgs:  []string{"[invalid"},
				},
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCommandValidator(tt.rules)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateEnv(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		allowed   []string
		shouldErr bool
	}{
		{
			name: "allowed env vars",
			env: map[string]string{
				"PATH": "/usr/bin",
				"HOME": "/home/user",
			},
			allowed:   []string{"PATH", "HOME", "USER"},
			shouldErr: false,
		},
		{
			name: "disallowed env var",
			env: map[string]string{
				"PATH":   "/usr/bin",
				"SECRET": "mysecret",
			},
			allowed:   []string{"PATH", "HOME"},
			shouldErr: true,
		},
		{
			name: "pattern matching",
			env: map[string]string{
				"LC_ALL":  "en_US.UTF-8",
				"LC_TIME": "en_US.UTF-8",
			},
			allowed:   []string{"LC_.*"},
			shouldErr: false,
		},
		{
			name: "no env vars allowed",
			env: map[string]string{
				"PATH": "/usr/bin",
			},
			allowed:   []string{},
			shouldErr: true,
		},
		{
			name:      "empty env with empty allowed",
			env:       map[string]string{},
			allowed:   []string{},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnv(tt.env, tt.allowed)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCommandValidator_MultipleRules(t *testing.T) {
	rules := []config.CommandRule{
		{
			Command:     "podman",
			Regex:       false,
			AllowedArgs: []string{"^(ps|images)$", ".*"},
			AllowPTY:    false,
		},
		{
			Command:     "docker",
			Regex:       false,
			AllowedArgs: []string{".*"},
			AllowPTY:    true,
		},
	}

	validator, err := NewCommandValidator(rules)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name      string
		cmd       string
		args      []string
		needsPTY  bool
		shouldErr bool
	}{
		{
			name:      "first rule podman ps",
			cmd:       "podman",
			args:      []string{"ps"},
			needsPTY:  false,
			shouldErr: false,
		},
		{
			name:      "first rule podman with PTY denied",
			cmd:       "podman",
			args:      []string{"ps"},
			needsPTY:  true,
			shouldErr: true,
		},
		{
			name:      "second rule docker",
			cmd:       "docker",
			args:      []string{"run"},
			needsPTY:  true,
			shouldErr: false,
		},
		{
			name:      "no matching rule",
			cmd:       "kubectl",
			args:      []string{"get", "pods"},
			needsPTY:  false,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cmd, tt.args, tt.needsPTY)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
