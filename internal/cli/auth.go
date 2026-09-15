package cli

import (
	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/loginclient"
	"github.com/spf13/cobra"
)

func (r *runner) authCommand(apiURL *string, getClient func() (*client.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "auth", Short: "Manage browser login"}
	root.AddCommand(&cobra.Command{Use: "login", Args: noArgs(), RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := loginclient.New(*apiURL)
		if err != nil {
			return err
		}
		if err = m.Login(cmd.Context(), cmd.ErrOrStderr()); err != nil {
			return err
		}
		r.result = map[string]any{"logged_in": true, "api_url": m.Base}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "logout", Args: noArgs(), RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := loginclient.New(*apiURL)
		if err != nil {
			return err
		}
		if err = m.Logout(cmd.Context()); err != nil {
			return err
		}
		r.result = map[string]bool{"logged_out": true}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "status", Args: noArgs(), RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := getClient()
		if err != nil {
			return err
		}
		v, err := c.Session(cmd.Context())
		if err != nil {
			return err
		}
		r.result = v
		return nil
	}})
	return root
}
