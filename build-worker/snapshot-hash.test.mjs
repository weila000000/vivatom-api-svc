import assert from "node:assert/strict"
import test from "node:test"
import { hashSnapshot, snapshotPayload } from "./snapshot-hash.mjs"

const contractSnapshot = {
  source: "model",
  title: "任务 & 看板",
  summary: "跨语言 <hash>\u2028contract",
  files: {
    "/src/main.tsx": "import App from './App'",
    "/src/App.tsx": "export default function App() { return <main>任务</main> }",
  },
  dependencies: { react: "18.3.1", "react-dom": "18.3.1" },
  entryFile: "/src/App.tsx",
}

test("matches the Go snapshot hash contract", () => {
  assert.equal(hashSnapshot(contractSnapshot), "70d25bbe3a5a4f56294ca4cd738fa5083740410808552c663ad4f79dde85580a")
  assert.match(snapshotPayload(contractSnapshot), /\\u003cmain\\u003e/)
  assert.match(snapshotPayload(contractSnapshot), /\\u0026/)
})

test("sorts map keys independently from insertion order", () => {
  const reordered = { ...contractSnapshot, files: Object.fromEntries(Object.entries(contractSnapshot.files).reverse()) }
  assert.equal(hashSnapshot(reordered), hashSnapshot(contractSnapshot))
})
