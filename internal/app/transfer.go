package app

import (
	"fmt"
	"io"
	"os"

	"github.com/yeck/easy-llm-router/internal/config"
	"github.com/yeck/easy-llm-router/internal/store"
	"github.com/yeck/easy-llm-router/internal/transfer"
)

// Export reads the existing config and secrets, encodes them as a base64
// bundle, and prints the result to stdout.
func Export() error {
	paths, err := store.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, err := store.LoadConfig(paths.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	secrets, err := store.LoadSecrets(paths.Credentials)
	if err != nil {
		return fmt.Errorf("load secrets: %w", err)
	}
	encoded, err := transfer.Encode(cfg, secrets)
	if err != nil {
		return err
	}
	fmt.Println(encoded)
	return nil
}

// Import decodes a base64 bundle from args or stdin, validates it, and writes
// the config and secrets to disk. It refuses to overwrite existing files unless
// force is true.
func Import(args []string) error {
	force := false
	var input string
	for _, arg := range args {
		if arg == "--force" || arg == "-f" {
			force = true
		} else if input == "" {
			input = arg
		}
	}
	if input == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		input = string(data)
	}
	if input == "" {
		return fmt.Errorf("no input: provide base64 as argument or via stdin")
	}

	bundle, err := transfer.Decode(input)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	config.ApplyDefaults(&bundle.Config)
	if err := config.Validate(bundle.Config, bundle.Secrets); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	paths, err := store.DefaultPaths()
	if err != nil {
		return err
	}

	if !force {
		if _, err := os.Stat(paths.Config); err == nil {
			return fmt.Errorf("config already exists at %s (use --force to overwrite)", paths.Config)
		}
		if _, err := os.Stat(paths.Credentials); err == nil {
			return fmt.Errorf("credentials already exist at %s (use --force to overwrite)", paths.Credentials)
		}
	}

	if err := store.SaveConfig(paths.Config, bundle.Config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := store.SaveSecrets(paths.Credentials, bundle.Secrets); err != nil {
		return fmt.Errorf("save secrets: %w", err)
	}
	// Reset runtime state so the next start begins cleanly.
	if err := store.SaveState(paths.State, store.NewState()); err != nil {
		return fmt.Errorf("reset state: %w", err)
	}

	fmt.Fprintln(os.Stderr, "configuration imported successfully")
	return nil
}
