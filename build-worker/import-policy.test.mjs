import assert from "node:assert/strict"
import test from "node:test"
import { createImportPolicy } from "./import-policy.mjs"

const allowed = createImportPolicy("/tmp/build/src")

test("allows Vue virtual modules that resolve to a source file", () => {
  assert.equal(allowed("vue", "/tmp/build/src/App.vue"), true)
  assert.equal(allowed("/src/components/Card.vue?raw", "/tmp/build/src/App.vue"), true)
  assert.equal(allowed("./components/Card.vue", "/tmp/build/src/App.vue"), true)
  assert.equal(allowed(
    "/tmp/build/src/App.vue?vue&type=script&setup=true&lang.ts",
    "/tmp/build/src/App.vue",
  ), true)
})

test("keeps imports outside the generated source directory denied", () => {
  assert.equal(allowed("axios", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("/tmp/secret.ts?raw", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("../../secret.ts", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("/src/../node_modules/pkg/index.js", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("/src/%2e%2e/node_modules/pkg/index.js", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("./%2e%2e/secret.ts", "/tmp/build/src/App.vue"), false)
  assert.equal(allowed("vue/runtime-dom", "/tmp/build/src/App.vue"), false)
})
