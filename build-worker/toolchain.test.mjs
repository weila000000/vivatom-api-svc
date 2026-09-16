import assert from "node:assert/strict"
import test from "node:test"
import { dirname } from "node:path"
import { describeToolchain, verifyToolchain } from "./toolchain.mjs"

const manifest = { dependencies: { vite: "7.3.6", "@vitejs/plugin-vue": "6.0.1", vue: "3.5.42" } }
const installed = { vite: "7.3.6", "@vitejs/plugin-vue": "6.0.1", vue: "3.5.42" }

test("identifies the complete installed build toolchain", () => {
  assert.equal(
    describeToolchain(manifest, installed, "22.22.0"),
    "node@22.22.0+vite@7.3.6+@vitejs/plugin-vue@6.0.1+vue@3.5.42",
  )
})

test("rejects ranges and installed version drift", () => {
  assert.throws(() => describeToolchain({ dependencies: { ...manifest.dependencies, vite: "^7.3.6" } }, installed, "22.22.0"), /toolchain_dependency_not_pinned:vite/)
  assert.throws(() => describeToolchain(manifest, { ...installed, vue: "3.6.0" }, "22.22.0"), /toolchain_version_mismatch:vue/)
})

test("verifies the worker installation on disk", async () => {
  const root = dirname(new URL(import.meta.url).pathname)
  assert.match(await verifyToolchain(root), /^node@\d+\.\d+\.\d+\+vite@7\.3\.6\+@vitejs\/plugin-vue@6\.0\.1\+vue@3\.5\.42$/)
})
