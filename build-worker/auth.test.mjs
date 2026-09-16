import assert from "node:assert/strict"
import test from "node:test"
import { builderTokenMatches, requireBuilderToken } from "./auth.mjs"

test("requires an explicit sufficiently long builder token", () => {
  assert.throws(() => requireBuilderToken({}), /at least 16 characters/)
  assert.throws(() => requireBuilderToken({ VIVATOM_BUILDER_TOKEN: "short" }), /at least 16 characters/)
  assert.equal(requireBuilderToken({ VIVATOM_BUILDER_TOKEN: "  builder-secret-123  " }), "builder-secret-123")
})

test("compares builder tokens without accepting type or length changes", () => {
  assert.equal(builderTokenMatches("builder-secret-123", "builder-secret-123"), true)
  assert.equal(builderTokenMatches("builder-secret-123", "builder-secret-124"), false)
  assert.equal(builderTokenMatches("builder-secret-123", "short"), false)
  assert.equal(builderTokenMatches("builder-secret-123", undefined), false)
})
