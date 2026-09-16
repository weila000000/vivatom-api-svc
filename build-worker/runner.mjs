import { build } from "vite"
import react from "@vitejs/plugin-react"
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
	plugins: [controlledImports, react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
})
