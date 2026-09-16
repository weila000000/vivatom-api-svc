export function artifactETag(fileHash) {
  return `"${fileHash}"`
}

function normalizeETag(value) {
  return value.trim().replace(/^W\//, "")
}

export function matchesIfNoneMatch(value, etag) {
  if (typeof value !== "string") return false
  return value.split(",").some((candidate) => candidate.trim() === "*" || normalizeETag(candidate) === etag)
}
