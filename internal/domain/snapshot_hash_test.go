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
			"/src/main.tsx": "import App from './App'",
			"/src/App.tsx":  "export default function App() { return <main>任务</main> }",
		},
		Dependencies: map[string]string{"react": "18.3.1", "react-dom": "18.3.1"},
		EntryFile:    "/src/App.tsx",
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
	const expected = "70d25bbe3a5a4f56294ca4cd738fa5083740410808552c663ad4f79dde85580a"
	if hash != expected {
		t.Fatalf("snapshot hash contract changed: got %s want %s", hash, expected)
	}
}
