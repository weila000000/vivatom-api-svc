package generation

import (
	"strings"
	"testing"

	"vivatom-api-svc/internal/domain"
)

func TestInjectRuntimeSDKAddsControlledBridgeClient(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Backend = domain.BackendSpec{Enabled: true, Auth: "none"}
	originalFiles := len(snapshot.Files)

	result := InjectRuntimeSDK(snapshot, `project-"one`)
	source := result.Files[RuntimeSDKPath]
	if len(result.Files) != originalFiles+1 || !strings.Contains(source, `project-\"one`) {
		t.Fatalf("runtime SDK was not injected safely: %q", source)
	}
	for _, forbidden := range []string{"fetch(", "Admin-Token", "publicKey"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("runtime SDK contains forbidden capability %q", forbidden)
		}
	}
	if _, exists := snapshot.Files[RuntimeSDKPath]; exists {
		t.Fatal("injection mutated provider snapshot")
	}
}

func TestInjectRuntimeSDKSkipsFrontendOnlySnapshot(t *testing.T) {
	snapshot := validSnapshot()
	result := InjectRuntimeSDK(snapshot, "project-1")
	if _, exists := result.Files[RuntimeSDKPath]; exists {
		t.Fatal("frontend-only snapshot received runtime SDK")
	}
}
