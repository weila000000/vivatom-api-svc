import { readFile } from "node:fs/promises"
import { join } from "node:path"
import { createHash } from "node:crypto"

export const toolchainPackages = ["vite", "@vitejs/plugin-react", "react", "react-dom", "lucide-react", "recharts", "date-fns", "typescript"]

export function describeToolchain(manifest, lockfile, installed, nodeVersion, lockHash) {
  const parts = [`node@${nodeVersion}`]
  for (const name of toolchainPackages) {
    const declared = manifest?.dependencies?.[name]
    const locked = lockfile?.packages?.[""]?.dependencies?.[name]
    const actual = installed[name]
    if (typeof declared !== "string" || !/^\d+\.\d+\.\d+$/.test(declared)) {
      throw new Error(`toolchain_dependency_not_pinned:${name}`)
    }
    if (locked !== declared) throw new Error(`toolchain_lock_mismatch:${name}:${declared}:${locked || "missing"}`)
    if (actual !== declared) throw new Error(`toolchain_version_mismatch:${name}:${declared}:${actual || "missing"}`)
    parts.push(`${name}@${actual}`)
  }
  if (!/^[a-f0-9]{64}$/.test(lockHash)) throw new Error("toolchain_lock_hash_invalid")
  parts.push(`lock@sha256:${lockHash}`)
  return parts.join("+")
}

export async function verifyToolchain(root, nodeVersion = process.versions.node) {
  const [manifestContent, lockContent] = await Promise.all([
    readFile(join(root, "package.json"), "utf8"),
    readFile(join(root, "package-lock.json"), "utf8"),
  ])
  const manifest = JSON.parse(manifestContent)
  const lockfile = JSON.parse(lockContent)
  const installed = {}
  for (const name of toolchainPackages) {
    const packageManifest = JSON.parse(await readFile(join(root, "node_modules", name, "package.json"), "utf8"))
    installed[name] = packageManifest.version
  }
  const lockHash = createHash("sha256").update(lockContent).digest("hex")
  return describeToolchain(manifest, lockfile, installed, nodeVersion, lockHash)
}
