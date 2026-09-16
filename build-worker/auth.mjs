import { timingSafeEqual } from "node:crypto"

export function requireBuilderToken(environment = process.env) {
  const token = environment.VIVATOM_BUILDER_TOKEN?.trim() || ""
  if (token.length < 16) throw new Error("VIVATOM_BUILDER_TOKEN must contain at least 16 characters")
  return token
}

export function builderTokenMatches(expected, received) {
  if (typeof received !== "string") return false
  const expectedBytes = Buffer.from(expected)
  const receivedBytes = Buffer.from(received)
  return expectedBytes.length === receivedBytes.length && timingSafeEqual(expectedBytes, receivedBytes)
}
