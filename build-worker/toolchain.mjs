import { readFile } from "node:fs/promises"
import { join } from "node:path"

export const toolchainPackages = ["vite", "@vitejs/plugin-vue", "vue"]

export function describeToolchain(manifest, installed, nodeVersion) {
  const parts = [`node@${nodeVersion}`]
  for (const name of toolchainPackages) {
    const declared = manifest?.dependencies?.[name]
    const actual = installed[name]
    if (typeof declared !== "string" || !/^\d+\.\d+\.\d+$/.test(declared)) {
      throw new Error(`toolchain_dependency_not_pinned:${name}`)
    }
    if (actual !== declared) throw new Error(`toolchain_version_mismatch:${name}:${declared}:${actual || "missing"}`)
    parts.push(`${name}@${actual}`)
  }
  return parts.join("+")
}

export async function verifyToolchain(root, nodeVersion = process.versions.node) {
  const manifest = JSON.parse(await readFile(join(root, "package.json"), "utf8"))
  const installed = {}
  for (const name of toolchainPackages) {
    const packageManifest = JSON.parse(await readFile(join(root, "node_modules", name, "package.json"), "utf8"))
    installed[name] = packageManifest.version
  }
  return describeToolchain(manifest, installed, nodeVersion)
}
