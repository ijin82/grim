package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldCleanupWorkspaces(t *testing.T) {
	root := &cobra.Command{Use: "grim"}
	completion := &cobra.Command{Use: "completion"}
	bash := &cobra.Command{Use: "bash"}
	completion.AddCommand(bash)
	root.AddCommand(completion)

	help := &cobra.Command{Use: "help"}
	root.AddCommand(help)

	complete := &cobra.Command{Use: "__complete"}
	root.AddCommand(complete)

	list := &cobra.Command{Use: "list"}
	root.AddCommand(list)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"root", root, true},
		{"ordinary command", list, true},
		{"completion parent", completion, false},
		{"completion shell", bash, false},
		{"help", help, false},
		{"cobra complete", complete, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCleanupWorkspaces(tc.cmd); got != tc.want {
				t.Fatalf("shouldCleanupWorkspaces(%q) = %v, want %v", tc.cmd.CommandPath(), got, tc.want)
			}
		})
	}
}
