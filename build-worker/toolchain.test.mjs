import assert from "node:assert/strict"
import test from "node:test"
import { dirname } from "node:path"
import { describeToolchain, verifyToolchain } from "./toolchain.mjs"

const manifest = { dependencies: { vite: "7.3.6", "@vitejs/plugin-react": "5.0.4", react: "18.3.1", "react-dom": "18.3.1", "lucide-react": "0.468.0", recharts: "2.13.3", "date-fns": "4.1.0", typescript: "5.9.3" } }
const lockfile = { packages: { "": { dependencies: { ...manifest.dependencies } } } }
const installed = { ...manifest.dependencies }
const lockHash = "a".repeat(64)

test("identifies the complete installed build toolchain", () => {
  assert.equal(
    describeToolchain(manifest, lockfile, installed, "22.22.0", lockHash),
	`node@22.22.0+vite@7.3.6+@vitejs/plugin-react@5.0.4+react@18.3.1+react-dom@18.3.1+lucide-react@0.468.0+recharts@2.13.3+date-fns@4.1.0+typescript@5.9.3+lock@sha256:${lockHash}`,
  )
})

test("rejects ranges and installed version drift", () => {
  assert.throws(() => describeToolchain({ dependencies: { ...manifest.dependencies, vite: "^7.3.6" } }, lockfile, installed, "22.22.0", lockHash), /toolchain_dependency_not_pinned:vite/)
	assert.throws(() => describeToolchain(manifest, lockfile, { ...installed, react: "19.0.0" }, "22.22.0", lockHash), /toolchain_version_mismatch:react/)
})

test("rejects a lockfile that does not match the manifest", () => {
  const drifted = { packages: { "": { dependencies: { ...manifest.dependencies, vite: "7.4.0" } } } }
  assert.throws(() => describeToolchain(manifest, drifted, installed, "22.22.0", lockHash), /toolchain_lock_mismatch:vite/)
})

test("verifies the worker installation on disk", async () => {
  const root = dirname(new URL(import.meta.url).pathname)
	assert.match(await verifyToolchain(root), /^node@\d+\.\d+\.\d+\+vite@7\.3\.6\+@vitejs\/plugin-react@5\.0\.4\+react@18\.3\.1\+react-dom@18\.3\.1\+lucide-react@0\.468\.0\+recharts@2\.13\.3\+date-fns@4\.1\.0\+typescript@5\.9\.3\+lock@sha256:[a-f0-9]{64}$/)
})
