import { build } from "vite"
import vue from "@vitejs/plugin-vue"
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path"

const root = process.argv[2]
const entryFile = process.argv[3]
if (!root || !entryFile) throw new Error("build root and entry are required")

const sourceRoot = join(root, "src")

function withinSource(path) {
  const local = relative(sourceRoot, path)
  return local === "" || (!isAbsolute(local) && local !== ".." && !local.startsWith(`..${sep}`))
}

const controlledImports = {
  name: "vivatom-controlled-imports",
  enforce: "pre",
  resolveId(source, importer) {
    if (!importer) return null
    const importerPath = importer.split("?", 1)[0]
    if (!withinSource(importerPath)) return null
    if (source === "vue" || source.startsWith("/src/")) return null
    if (source.startsWith(".") && withinSource(resolve(dirname(importerPath), source))) return null
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
