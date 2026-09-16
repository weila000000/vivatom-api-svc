import { dirname, isAbsolute, relative, resolve, sep } from "node:path"

export function createImportPolicy(sourceRoot) {
  const canonicalSourceRoot = resolve(sourceRoot)

  function withinSource(path) {
    const local = relative(canonicalSourceRoot, path)
    return local === "" || (!isAbsolute(local) && local !== ".." && !local.startsWith(`..${sep}`))
  }

  function decodePath(path) {
    try {
      const decoded = decodeURIComponent(path)
      return decoded.includes("\\") || decoded.includes("\0") ? undefined : decoded
    } catch {
      return undefined
    }
  }

  return function importAllowed(source, importer) {
    if (!importer) return true
    const importerPath = importer.split("?", 1)[0]
    if (!withinSource(importerPath)) return true
	if (["react", "react/jsx-runtime", "react/jsx-dev-runtime", "react-dom", "react-dom/client", "lucide-react", "recharts", "date-fns"].includes(source)) return true

    const sourcePath = decodePath(source.split("?", 1)[0])
    if (!sourcePath) return false
    if (sourcePath.startsWith("/src/")) {
      return withinSource(resolve(canonicalSourceRoot, sourcePath.slice("/src/".length)))
    }
    if (isAbsolute(sourcePath) && withinSource(sourcePath)) return true
    return source.startsWith(".") && withinSource(resolve(dirname(importerPath), sourcePath))
  }
}
