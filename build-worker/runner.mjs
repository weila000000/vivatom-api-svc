import { build } from "vite"
import vue from "@vitejs/plugin-vue"
import { join } from "node:path"
import { createImportPolicy } from "./import-policy.mjs"

const root = process.argv[2]
const entryFile = process.argv[3]
if (!root || !entryFile) throw new Error("build root and entry are required")

const sourceRoot = join(root, "src")
const importAllowed = createImportPolicy(sourceRoot)

const controlledImports = {
  name: "vivatom-controlled-imports",
  enforce: "pre",
  resolveId(source, importer) {
    if (importAllowed(source, importer)) return null
    this.error(`dependency denied: ${source}`)
  },
}

await build({
  root,
  base: "./",
  configFile: false,
  plugins: [controlledImports, vue()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
})
