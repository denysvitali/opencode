import path from "path"
import { Global } from "../global"
import z from "zod"

export const OAUTH_DUMMY_KEY = "opencode-oauth-dummy-key"

export namespace Auth {
  export const Oauth = z
    .object({
      type: z.literal("oauth"),
      refresh: z.string(),
      access: z.string(),
      expires: z.number(),
      accountId: z.string().optional(),
      enterpriseUrl: z.string().optional(),
    })
    .meta({ ref: "OAuth" })

  export const Api = z
    .object({
      type: z.literal("api"),
      key: z.string(),
    })
    .meta({ ref: "ApiAuth" })

  export const WellKnown = z
    .object({
      type: z.literal("wellknown"),
      key: z.string(),
      token: z.string(),
    })
    .meta({ ref: "WellKnownAuth" })

  export const Info = z.discriminatedUnion("type", [Oauth, Api, WellKnown]).meta({ ref: "Auth" })
  export type Info = z.infer<typeof Info>

  const filepath = path.join(Global.Path.data, "auth.json")

  function sanitizeApiKey(input: string) {
    return input.replace(/[\r\n]+/g, "").trim()
  }

  function sanitizeInfo(info: Info): Info {
    if (info.type !== "api") return info
    return {
      ...info,
      key: sanitizeApiKey(info.key),
    }
  }

  export async function get(providerID: string) {
    const auth = await all()
    return auth[providerID]
  }

  export async function all(): Promise<Record<string, Info>> {
    const file = Bun.file(filepath)
    const data = await file.json().catch(() => ({}) as Record<string, unknown>)
    let changed = false
    const result = Object.entries(data).reduce((acc, [key, value]) => {
      const parsed = Info.safeParse(value)
      if (!parsed.success) return acc
      const next = sanitizeInfo(parsed.data)
      if (!changed && JSON.stringify(next) !== JSON.stringify(parsed.data)) changed = true
      acc[key] = next
      return acc
    }, {} as Record<string, Info>)
    if (changed) {
      await Bun.write(file, JSON.stringify(result, null, 2), { mode: 0o600 })
    }
    return result
  }

  export async function set(key: string, info: Info) {
    const file = Bun.file(filepath)
    const data = await all()
    await Bun.write(file, JSON.stringify({ ...data, [key]: sanitizeInfo(info) }, null, 2), { mode: 0o600 })
  }

  export async function remove(key: string) {
    const file = Bun.file(filepath)
    const data = await all()
    delete data[key]
    await Bun.write(file, JSON.stringify(data, null, 2), { mode: 0o600 })
  }
}
