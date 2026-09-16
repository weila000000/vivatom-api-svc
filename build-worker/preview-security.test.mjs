import assert from "node:assert/strict"
import test from "node:test"
import { previewSecurityHeaders } from "./preview-security.mjs"

test("isolates generated previews from browser capabilities", () => {
  const policy = previewSecurityHeaders["Content-Security-Policy"]
  for (const directive of [
    "sandbox allow-scripts",
    "default-src 'none'",
    "connect-src 'none'",
    "worker-src 'none'",
    "frame-src 'none'",
    "form-action 'none'",
    "object-src 'none'",
  ]) {
    assert.ok(policy.includes(directive), `missing ${directive}`)
  }
  assert.equal(policy.includes("allow-same-origin"), false)
  assert.equal(previewSecurityHeaders["Cross-Origin-Opener-Policy"], "same-origin")
  assert.equal(previewSecurityHeaders["Access-Control-Allow-Origin"], "*")
  assert.equal(previewSecurityHeaders["Referrer-Policy"], "no-referrer")
  assert.match(previewSecurityHeaders["Permissions-Policy"], /camera=\(\)/)
  assert.match(previewSecurityHeaders["Permissions-Policy"], /microphone=\(\)/)
})
