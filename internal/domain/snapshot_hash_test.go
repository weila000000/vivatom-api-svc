package domain

import "testing"

func TestHashSnapshotChangesWithSource(t *testing.T) {
	snapshot := ProjectSnapshot{Source: "template", Title: "Task", Files: map[string]string{"/src/main.ts": "export {}"}, Dependencies: map[string]string{"vue": "3.5.42"}, EntryFile: "/src/main.ts"}
	first, payload, err := HashSnapshot(snapshot)
	if err != nil || len(first) != 64 || len(payload) == 0 {
		t.Fatalf("hash snapshot: hash=%q payload=%q err=%v", first, payload, err)
	}
	snapshot.Files["/src/main.ts"] = "export const changed = true"
	second, _, err := HashSnapshot(snapshot)
	if err != nil || second == first {
		t.Fatalf("changed snapshot hash=%q original=%q err=%v", second, first, err)
	}
}

func TestHashSnapshotMatchesWorkerContract(t *testing.T) {
	snapshot := ProjectSnapshot{
		Source:  "model",
		Title:   "任务 & 看板",
		Summary: "跨语言 <hash>\u2028contract",
		Files: map[string]string{
			"/src/main.js": "import App from './App.vue'",
			"/src/App.vue":  "<template><main>任务</main></template>",
		},
		Dependencies: map[string]string{"vue": "3.5.42"},
		EntryFile:    "/src/main.js",
		Backend: BackendSpec{
			Enabled: true,
			Auth:    "tenant",
			Collections: []BackendCollection{{
				Name: "tasks", Label: "任务", Access: "member",
				Fields: []BackendField{{Name: "title", Label: "标题", Type: "text", Required: true}},
			}},
		},
	}
	hash, _, err := HashSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "c45fc3ae0daf2695fd5dbad7733b07ec5dfaef79bab916761ff37d07f109b4a6"
	if hash != expected {
		t.Fatalf("snapshot hash contract changed: got %s want %s", hash, expected)
	}
}
