import { createHash } from "node:crypto"

function encodeString(value) {
  return JSON.stringify(String(value)).replace(/[<>&\u2028\u2029]/g, (character) => ({
    "<": "\\u003c",
    ">": "\\u003e",
    "&": "\\u0026",
    "\u2028": "\\u2028",
    "\u2029": "\\u2029",
  })[character])
}

function encodeMap(values) {
  if (values == null) return "null"
  return `{${Object.keys(values).sort().map((key) => `${encodeString(key)}:${encodeString(values[key])}`).join(",")}}`
}

export function snapshotPayload(snapshot) {
  return `{"source":${encodeString(snapshot?.source ?? "")},"title":${encodeString(snapshot?.title ?? "")},"summary":${encodeString(snapshot?.summary ?? "")},"files":${encodeMap(snapshot?.files)},"dependencies":${encodeMap(snapshot?.dependencies)},"entryFile":${encodeString(snapshot?.entryFile ?? "")}}`
}

export function hashSnapshot(snapshot) {
  return createHash("sha256").update(snapshotPayload(snapshot)).digest("hex")
}
