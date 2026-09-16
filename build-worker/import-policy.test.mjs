import assert from "node:assert/strict"
import test from "node:test"
import { createImportPolicy } from "./import-policy.mjs"

const allowed = createImportPolicy("/tmp/build/src")

test("allows React packages and local source modules", () => {
  assert.equal(allowed("react", "/tmp/build/src/App.tsx"), true)
	assert.equal(allowed("react/jsx-runtime", "/tmp/build/src/App.tsx"), true)
  assert.equal(allowed("react-dom/client", "/tmp/build/src/main.tsx"), true)
  assert.equal(allowed("/src/components/Card.tsx?raw", "/tmp/build/src/App.tsx"), true)
  assert.equal(allowed("./components/Card", "/tmp/build/src/App.tsx"), true)
})

test("keeps imports outside the generated source directory denied", () => {
	assert.equal(allowed("axios", "/tmp/build/src/App.tsx"), false)
  assert.equal(allowed("/tmp/secret.ts?raw", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("../../secret.ts", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("/src/../node_modules/pkg/index.js", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("/src/%2e%2e/node_modules/pkg/index.js", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("./%2e%2e/secret.ts", "/tmp/build/src/App.vue"), false)
	assert.equal(allowed("react-router-dom", "/tmp/build/src/App.tsx"), false)
})
