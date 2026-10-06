package launcher

import (
	"testing"

	"github.com/telecter/cmd-launcher/internal/meta"
	"github.com/telecter/cmd-launcher/pkg/auth"
)

// argsContainPair reports whether args holds name immediately followed by value.
func argsContainPair(args []string, name, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

// TestCreateArgsOfflineSession checks that an offline session, which carries no access token,
// still reaches the game arguments.
func TestCreateArgsOfflineSession(t *testing.T) {
	options := LaunchOptions{
		Session: auth.Session{Username: "Steve"},
		InstanceConfig: InstanceConfig{
			WindowResolution: Resolution{Width: 1920, Height: 1080},
		},
	}

	_, game := createArgs(LaunchEnvironment{GameDir: t.TempDir()}, meta.VersionMeta{}, options, "")

	if !argsContainPair(game, "--username", "Steve") {
		t.Fatalf("expected --username Steve, got %v", game)
	}
	if !argsContainPair(game, "--accessToken", "") {
		t.Fatalf("expected an empty access token for an offline session, got %v", game)
	}
	if argsContainPair(game, "--uuid", "") {
		t.Fatalf("expected no --uuid for an offline session, got %v", game)
	}
}

// TestCreateArgsCustomInfoAndAutoJoin checks that the PCL-style settings actually change the
// arguments the game is started with.
func TestCreateArgsCustomInfoAndAutoJoin(t *testing.T) {
	options := LaunchOptions{
		Session: auth.Session{Username: "Steve"},
		InstanceConfig: InstanceConfig{
			WindowResolution: Resolution{Width: 1920, Height: 1080},
			CustomInfo:       "Hello there",
			AutoJoinServer:   "example.org",
			WindowTitle:      "My title",
		},
	}

	java, game := createArgs(LaunchEnvironment{GameDir: t.TempDir()}, meta.VersionMeta{Type: "release"}, options, "")

	if !argsContainPair(game, "--versionType", "Hello there") {
		t.Fatalf("expected custom info to replace the version type, got %v", game)
	}
	if !argsContainPair(game, "--quickPlayMultiplayer", "example.org") {
		t.Fatalf("expected the auto join server, got %v", game)
	}
	if !argsContain(java, "-Dminecraft.windowTitle=My title") {
		t.Fatalf("expected the window title property, got %v", java)
	}
}

// argsContain reports whether args holds the given element.
func argsContain(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
