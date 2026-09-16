import assert from "node:assert/strict"
import test from "node:test"
import { hashSnapshot, snapshotPayload } from "./snapshot-hash.mjs"

const contractSnapshot = {
  source: "model",
  title: "任务 & 看板",
  summary: "跨语言 <hash>\u2028contract",
  files: {
    "/src/main.js": "import App from './App.vue'",
    "/src/App.vue": "<template><main>任务</main></template>",
  },
  dependencies: { vue: "3.5.42" },
  entryFile: "/src/main.js",
  backend: {
    enabled: true,
    auth: "tenant",
    collections: [{
      name: "tasks",
      label: "任务",
      access: "member",
      fields: [{ name: "title", label: "标题", type: "text", required: true }],
    }],
  },
}

test("matches the Go snapshot hash contract", () => {
  assert.equal(hashSnapshot(contractSnapshot), "c45fc3ae0daf2695fd5dbad7733b07ec5dfaef79bab916761ff37d07f109b4a6")
  assert.match(snapshotPayload(contractSnapshot), /\\u003ctemplate\\u003e/)
  assert.match(snapshotPayload(contractSnapshot), /\\u0026/)
})

test("sorts map keys independently from insertion order", () => {
  const reordered = { ...contractSnapshot, files: Object.fromEntries(Object.entries(contractSnapshot.files).reverse()) }
  assert.equal(hashSnapshot(reordered), hashSnapshot(contractSnapshot))
})
