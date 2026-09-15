import { join } from "node:path"
import { build } from "vite"
import vue from "@vitejs/plugin-vue"

const root = process.argv[2]
const entryFile = process.argv[3]
if (!root || !entryFile) throw new Error("build root and entry are required")

await build({
  root,
  configFile: false,
  plugins: [vue()],
  build: {
    outDir: join(root, "dist"),
    emptyOutDir: true,
    lib: {
      entry: join(root, entryFile.replace(/^\//, "")),
      formats: ["es"],
      fileName: "app",
    },
  },
})
