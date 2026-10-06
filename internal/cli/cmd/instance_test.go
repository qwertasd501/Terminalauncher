package cmd

import (
	"errors"
	"testing"

	"github.com/qwertasd501/Terminalauncher/internal/meta"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

// TestCreateFetchesTheFilesItNeeds pins the point of CreateCmd: creating an instance also fetches
// the files it needs in order to start, so that the first launch goes straight to the game.
//
// Both steps that reach the network are replaced, so the test stays offline.
func TestCreateFetchesTheFilesItNeeds(t *testing.T) {
	created := launcher.Instance{
		Name:        "1.20.1-Forge_47.4.6",
		GameVersion: "1.20.1",
		Loader:      meta.LoaderForge,

		Config: launcher.DefaultInstanceConfig(),
	}

	cases := []struct {
		name      string
		noFill    bool
		createErr error
		wantFill  bool
		wantErr   bool
	}{
		{name: "the files are fetched while the instance is created", wantFill: true},
		{name: "--no-fill leaves them for the first launch", noFill: true},
		{name: "a failed creation fetches nothing", createErr: errors.New("no such version"), wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			previousCreate, previousFill := createInstance, fillInstance
			t.Cleanup(func() { createInstance, fillInstance = previousCreate, previousFill })

			createInstance = func(launcher.InstanceOptions) (launcher.Instance, error) {
				if c.createErr != nil {
					return launcher.Instance{}, c.createErr
				}
				return created, nil
			}
			var filled []string
			fillInstance = func(inst launcher.Instance, verbosity int) {
				filled = append(filled, inst.Name)
			}

			cmd := &CreateCmd{Version: "1.20.1", Loader: "forge", LoaderVersion: "47.4.6", NoFill: c.noFill}
			err := cmd.Run(nil, 0)

			if c.wantErr != (err != nil) {
				t.Fatalf("Run() error = %v, want error: %v", err, c.wantErr)
			}
			if c.wantFill {
				if len(filled) != 1 || filled[0] != created.Name {
					t.Fatalf("the files fetched were %v, want exactly %q", filled, created.Name)
				}
			} else if len(filled) != 0 {
				t.Fatalf("nothing should have been fetched, but %v was", filled)
			}
		})
	}
}
