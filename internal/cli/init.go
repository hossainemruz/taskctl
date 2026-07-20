package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/spf13/cobra"
)

func newInitCommand(initializer InitService) *cobra.Command {
	var vaultPath string
	var viewerCommand string
	var viewerArgs []string
	var nonInteractive bool
	var force bool

	command := &cobra.Command{
		Use:   "init",
		Short: "Configure this machine and initialize a taskctl vault",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			defaults, err := initializer.Defaults(command.Context())
			if err != nil {
				return err
			}

			reader := bufio.NewReader(command.InOrStdin())
			vault := strings.TrimSpace(vaultPath)
			if vault == "" && defaults.Found {
				vault = defaults.Vault
			}
			if vault == "" {
				if nonInteractive {
					return app.NewError(app.ErrorUsage, "--vault is required when no local configuration exists")
				}
				vault, err = prompt(reader, command.ErrOrStderr(), "Vault path", "")
				if err != nil {
					return err
				}
			}

			viewer := strings.TrimSpace(viewerCommand)
			viewerFlagChanged := command.Flags().Changed("viewer")
			viewerArgsChanged := command.Flags().Changed("viewer-arg")
			if viewer == "" && defaults.Found {
				viewer = defaults.Viewer.Command
			}
			if viewer == "" {
				if nonInteractive {
					return app.NewError(app.ErrorUsage, "--viewer is required when no local configuration exists")
				}
				viewer, err = prompt(reader, command.ErrOrStderr(), "Viewer executable", "typora")
				if err != nil {
					return err
				}
			}

			args := append([]string(nil), viewerArgs...)
			if !viewerArgsChanged && defaults.Found && !viewerFlagChanged {
				args = append([]string(nil), defaults.Viewer.Args...)
			}

			result, err := initializer.Init(command.Context(), app.InitInput{
				Vault: vault,
				Viewer: config.Viewer{
					Command: viewer,
					Args:    args,
				},
				Force: force,
			})
			if err != nil {
				return err
			}

			state := "ready"
			if result.VaultCreated {
				state = "created"
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"Vault %s: %s\nConfiguration saved: %s\nTemplates installed: %d\n",
				state,
				result.Vault,
				result.ConfigPath,
				len(result.TemplatesInstalled),
			)
			if err != nil {
				return app.WrapError(app.ErrorInternal, err, "write initialization result: %v", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&vaultPath, "vault", "", "vault directory (prompted when omitted)")
	command.Flags().StringVar(&viewerCommand, "viewer", "", "Markdown viewer executable (prompted when omitted)")
	command.Flags().StringArrayVar(&viewerArgs, "viewer-arg", nil, "viewer argument; repeat for multiple arguments")
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "fail instead of prompting for missing values")
	command.Flags().BoolVar(&force, "force", false, "initialize a non-empty directory that has no taskctl.yaml")
	return command
}

func prompt(reader *bufio.Reader, output io.Writer, label, defaultValue string) (string, error) {
	var err error
	if defaultValue == "" {
		_, err = fmt.Fprintf(output, "%s: ", label)
	} else {
		_, err = fmt.Fprintf(output, "%s [%s]: ", label, defaultValue)
	}
	if err != nil {
		return "", app.WrapError(app.ErrorInternal, err, "write %s prompt: %v", strings.ToLower(label), err)
	}
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", app.WrapError(app.ErrorInternal, err, "read %s: %v", strings.ToLower(label), err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return "", app.NewError(app.ErrorUsage, "%s is required", strings.ToLower(label))
	}
	return value, nil
}
