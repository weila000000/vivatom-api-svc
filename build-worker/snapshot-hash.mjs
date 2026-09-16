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

function encodeArray(values, encode) {
  if (values == null) return "null"
  return `[${values.map(encode).join(",")}]`
}

function encodeMap(values) {
  if (values == null) return "null"
  return `{${Object.keys(values).sort().map((key) => `${encodeString(key)}:${encodeString(values[key])}`).join(",")}}`
}

function encodeField(field) {
  return `{"name":${encodeString(field?.name ?? "")},"label":${encodeString(field?.label ?? "")},"type":${encodeString(field?.type ?? "")},"required":${field?.required === true}}`
}

function encodeCollection(collection) {
  return `{"name":${encodeString(collection?.name ?? "")},"label":${encodeString(collection?.label ?? "")},"access":${encodeString(collection?.access ?? "")},"fields":${encodeArray(collection?.fields, encodeField)}}`
}

function encodeBackend(backend) {
  return `{"enabled":${backend?.enabled === true},"auth":${encodeString(backend?.auth ?? "")},"collections":${encodeArray(backend?.collections, encodeCollection)}}`
}

export function snapshotPayload(snapshot) {
  return `{"source":${encodeString(snapshot?.source ?? "")},"title":${encodeString(snapshot?.title ?? "")},"summary":${encodeString(snapshot?.summary ?? "")},"files":${encodeMap(snapshot?.files)},"dependencies":${encodeMap(snapshot?.dependencies)},"entryFile":${encodeString(snapshot?.entryFile ?? "")},"backend":${encodeBackend(snapshot?.backend)}}`
}

export function hashSnapshot(snapshot) {
  return createHash("sha256").update(snapshotPayload(snapshot)).digest("hex")
}
