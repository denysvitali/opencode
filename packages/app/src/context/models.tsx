import { createMemo } from "solid-js"
import { createStore } from "solid-js/store"
import { DateTime } from "luxon"
import { filter, firstBy, flat, groupBy, mapValues, pipe, uniqueBy, values } from "remeda"
import { createSimpleContext } from "@opencode-ai/ui/context"
import { useProviders } from "@/hooks/use-providers"
import { Persist, persisted } from "@/utils/persist"

export type ModelKey = { providerID: string; modelID: string }

type Visibility = "show" | "hide"
type User = ModelKey & { visibility: Visibility; favorite?: boolean }

export type CustomModel = {
  id: string
  providerID: string
  name: string
  cost: {
    input: number
    output: number
    cache_read?: number
    cache_write?: number
  }
  limit: {
    context: number
    output?: number
  }
}

type Store = {
  user: User[]
  recent: ModelKey[]
  variant?: Record<string, string | undefined>
  custom: CustomModel[]
}

export const { use: useModels, provider: ModelsProvider } = createSimpleContext({
  name: "Models",
  init: () => {
    const providers = useProviders()

    const [store, setStore, _, ready] = persisted(
      Persist.global("model", ["model.v1"]),
      createStore<Store>({
        user: [],
        recent: [],
        variant: {},
        custom: [],
      }),
    )

    const available = createMemo(() =>
      providers.connected().flatMap((p) =>
        Object.values(p.models).map((m) => ({
          ...m,
          provider: p,
        })),
      ),
    )

    const latest = createMemo(() =>
      pipe(
        available(),
        filter((x) => Math.abs(DateTime.fromISO(x.release_date).diffNow().as("months")) < 6),
        groupBy((x) => x.provider.id),
        mapValues((models) =>
          pipe(
            models,
            groupBy((x) => x.family),
            values(),
            (groups) =>
              groups.flatMap((g) => {
                const first = firstBy(g, [(x) => x.release_date, "desc"])
                return first ? [{ modelID: first.id, providerID: first.provider.id }] : []
              }),
          ),
        ),
        values(),
        flat(),
      ),
    )

    const latestSet = createMemo(() => new Set(latest().map((x) => `${x.providerID}:${x.modelID}`)))

    const visibility = createMemo(() => {
      const map = new Map<string, Visibility>()
      for (const item of store.user) map.set(`${item.providerID}:${item.modelID}`, item.visibility)
      return map
    })

    const customList = createMemo(() =>
      store.custom.map((custom) => {
        const provider = providers.all().find((p) => p.id === custom.providerID)
        return {
          id: `custom:${custom.id}`,
          name: custom.name,
          provider: provider ?? { id: custom.providerID, name: custom.providerID },
          cost: custom.cost,
          limit: custom.limit,
          release_date: new Date().toISOString(),
          attachment: false,
          reasoning: false,
          temperature: true,
          tool_call: false,
          custom: true,
          customData: custom,
        }
      }),
    )

    const list = createMemo(() => {
      const official = available().map((m) => ({
        ...m,
        name: m.name.replace("(latest)", "").trim(),
        latest: m.name.includes("(latest)"),
      }))
      return [...official, ...customList()]
    })

    const find = (key: ModelKey) => list().find((m) => m.id === key.modelID && m.provider.id === key.providerID)

    function update(model: ModelKey, state: Visibility) {
      const index = store.user.findIndex((x) => x.modelID === model.modelID && x.providerID === model.providerID)
      if (index >= 0) {
        setStore("user", index, { visibility: state })
        return
      }
      setStore("user", store.user.length, { ...model, visibility: state })
    }

    const visible = (model: ModelKey) => {
      const key = `${model.providerID}:${model.modelID}`
      const state = visibility().get(key)
      if (state === "hide") return false
      if (state === "show") return true
      if (latestSet().has(key)) return true
      const m = find(model)
      if (m && "custom" in m && m.custom) return true
      if (!m?.release_date || !DateTime.fromISO(m.release_date).isValid) return true
      return false
    }

    const setVisibility = (model: ModelKey, state: boolean) => {
      update(model, state ? "show" : "hide")
    }

    const push = (model: ModelKey) => {
      const uniq = uniqueBy([model, ...store.recent], (x) => x.providerID + x.modelID)
      if (uniq.length > 5) uniq.pop()
      setStore("recent", uniq)
    }

    const variantKey = (model: ModelKey) => `${model.providerID}/${model.modelID}`
    const getVariant = (model: ModelKey) => store.variant?.[variantKey(model)]

    const setVariant = (model: ModelKey, value: string | undefined) => {
      const key = variantKey(model)
      if (!store.variant) {
        setStore("variant", { [key]: value })
        return
      }
      setStore("variant", key, value)
    }

    const addCustom = (model: CustomModel) => {
      setStore("custom", store.custom.length, model)
    }

    const updateCustom = (index: number, model: CustomModel) => {
      setStore("custom", index, model)
    }

    const removeCustom = (index: number) => {
      const custom = store.custom[index]
      if (!custom) return
      const modelKey = { providerID: custom.providerID, modelID: `custom:${custom.id}` }
      setStore(
        "custom",
        store.custom.filter((_, i) => i !== index),
      )
      setStore(
        "user",
        store.user.filter((u) => !(u.providerID === modelKey.providerID && u.modelID === modelKey.modelID)),
      )
      setStore(
        "recent",
        store.recent.filter((r) => !(r.providerID === modelKey.providerID && r.modelID === modelKey.modelID)),
      )
    }

    const findCustomIndex = (id: string, providerID: string) =>
      store.custom.findIndex((c) => c.id === id && c.providerID === providerID)

    return {
      ready,
      list,
      find,
      visible,
      setVisibility,
      recent: {
        list: createMemo(() => store.recent),
        push,
      },
      variant: {
        get: getVariant,
        set: setVariant,
      },
      custom: {
        list: createMemo(() => store.custom),
        add: addCustom,
        update: updateCustom,
        remove: removeCustom,
        findIndex: findCustomIndex,
      },
    }
  },
})
