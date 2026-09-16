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
