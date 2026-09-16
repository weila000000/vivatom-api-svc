import assert from "node:assert/strict"
import test from "node:test"
import { artifactETag, matchesIfNoneMatch } from "./preview-http.mjs"

test("creates a strong ETag from the verified file hash", () => {
  assert.equal(artifactETag("abc123"), '"abc123"')
})

test("matches standard If-None-Match variants", () => {
  const etag = artifactETag("abc123")
  assert.equal(matchesIfNoneMatch('"other", W/"abc123"', etag), true)
  assert.equal(matchesIfNoneMatch("*", etag), true)
  assert.equal(matchesIfNoneMatch('"other"', etag), false)
  assert.equal(matchesIfNoneMatch(undefined, etag), false)
})
