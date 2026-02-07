import { useFilteredList } from "@opencode-ai/ui/hooks"
import { ProviderIcon } from "@opencode-ai/ui/provider-icon"
import { Switch } from "@opencode-ai/ui/switch"
import { Icon } from "@opencode-ai/ui/icon"
import { IconButton } from "@opencode-ai/ui/icon-button"
import { TextField } from "@opencode-ai/ui/text-field"
import { Button } from "@opencode-ai/ui/button"
import { Tag } from "@opencode-ai/ui/tag"
import type { IconName } from "@opencode-ai/ui/icons/provider"
import { type Component, For, Show } from "solid-js"
import { useDialog } from "@opencode-ai/ui/context/dialog"
import { useLanguage } from "@/context/language"
import { useModels } from "@/context/models"
import { popularProviders } from "@/hooks/use-providers"
import { DialogAddCustomModel } from "./dialog-add-custom-model"

type ModelItem = ReturnType<ReturnType<typeof useModels>["list"]>[number]

export const SettingsModels: Component = () => {
  const language = useLanguage()
  const models = useModels()
  const dialog = useDialog()

  const handleAddCustom = () => {
    dialog.show(() => <DialogAddCustomModel />)
  }

  const handleEditCustom = (index: number) => {
    const custom = models.custom.list()[index]
    if (!custom) return
    dialog.show(() => <DialogAddCustomModel edit={{ ...custom, index }} />)
  }

  const handleDeleteCustom = (index: number) => {
    const custom = models.custom.list()[index]
    if (!custom) return
    if (confirm(language.t("model.custom.delete.confirm", { name: custom.name }))) {
      models.custom.remove(index)
    }
  }

  const list = useFilteredList<ModelItem>({
    items: (_filter) => models.list(),
    key: (x) => `${x.provider.id}:${x.id}`,
    filterKeys: ["provider.name", "name", "id"],
    sortBy: (a, b) => a.name.localeCompare(b.name),
    groupBy: (x) => x.provider.id,
    sortGroupsBy: (a, b) => {
      const aIndex = popularProviders.indexOf(a.category)
      const bIndex = popularProviders.indexOf(b.category)
      const aPopular = aIndex >= 0
      const bPopular = bIndex >= 0

      if (aPopular && !bPopular) return -1
      if (!aPopular && bPopular) return 1
      if (aPopular && bPopular) return aIndex - bIndex

      const aName = a.items[0].provider.name
      const bName = b.items[0].provider.name
      return aName.localeCompare(bName)
    },
  })

  return (
    <div class="flex flex-col h-full overflow-y-auto no-scrollbar px-4 pb-10 sm:px-10 sm:pb-10">
      <div class="sticky top-0 z-10 bg-[linear-gradient(to_bottom,var(--surface-raised-stronger-non-alpha)_calc(100%_-_24px),transparent)]">
        <div class="flex flex-col gap-4 pt-6 pb-6 max-w-[720px]">
          <h2 class="text-16-medium text-text-strong">{language.t("settings.models.title")}</h2>
          <div class="flex items-center gap-2 px-3 h-9 rounded-lg bg-surface-base">
            <Icon name="magnifying-glass" class="text-icon-weak-base flex-shrink-0" />
            <TextField
              variant="ghost"
              type="text"
              value={list.filter()}
              onChange={list.onInput}
              placeholder={language.t("dialog.model.search.placeholder")}
              spellcheck={false}
              autocorrect="off"
              autocomplete="off"
              autocapitalize="off"
              class="flex-1"
            />
            <Show when={list.filter()}>
              <IconButton icon="circle-x" variant="ghost" onClick={list.clear} />
            </Show>
          </div>
        </div>
      </div>

      <div class="flex flex-col gap-8 max-w-[720px]">
        <Show
          when={!list.grouped.loading}
          fallback={
            <div class="flex flex-col items-center justify-center py-12 text-center">
              <span class="text-14-regular text-text-weak">
                {language.t("common.loading")}
                {language.t("common.loading.ellipsis")}
              </span>
            </div>
          }
        >
          <Show
            when={list.flat().length > 0}
            fallback={
              <div class="flex flex-col items-center justify-center py-12 text-center">
                <span class="text-14-regular text-text-weak">{language.t("dialog.model.empty")}</span>
                <Show when={list.filter()}>
                  <span class="text-14-regular text-text-strong mt-1">&quot;{list.filter()}&quot;</span>
                </Show>
              </div>
            }
          >
            <For each={list.grouped.latest}>
              {(group) => (
                <div class="flex flex-col gap-1">
                  <div class="flex items-center gap-2 pb-2">
                    <ProviderIcon id={group.category as IconName} class="size-5 shrink-0 icon-strong-base" />
                    <span class="text-14-medium text-text-strong">{group.items[0].provider.name}</span>
                  </div>
                  <div class="bg-surface-raised-base px-4 rounded-lg">
                    <For each={group.items}>
                      {(item) => {
                        const key = { providerID: item.provider.id, modelID: item.id }
                        return (
                          <div class="flex flex-wrap items-center justify-between gap-4 py-3 border-b border-border-weak-base last:border-none">
                            <div class="min-w-0">
                              <span class="text-14-regular text-text-strong truncate block">{item.name}</span>
                            </div>
                            <div class="flex-shrink-0">
                              <Switch
                                checked={models.visible(key)}
                                onChange={(checked) => {
                                  models.setVisibility(key, checked)
                                }}
                                hideLabel
                              >
                                {item.name}
                              </Switch>
                            </div>
                          </div>
                        )
                      }}
                    </For>
                  </div>
                </div>
              )}
            </For>
          </Show>
        </Show>
      </div>

      <Show when={models.custom.list().length > 0}>
        <div class="flex flex-col gap-4 max-w-[720px]">
          <div class="flex items-center justify-between">
            <div class="flex flex-col gap-1">
              <h3 class="text-14-medium text-text-strong">{language.t("model.custom.section.title")}</h3>
              <p class="text-12-regular text-text-weak">{language.t("model.custom.section.description")}</p>
            </div>
            <Button variant="ghost" size="small" icon="plus-small" onClick={handleAddCustom}>
              {language.t("model.custom.add.button")}
            </Button>
          </div>
          <div class="bg-surface-raised-base px-4 rounded-lg">
            <For each={models.custom.list()}>
              {(item, index) => (
                <div class="flex flex-wrap items-center justify-between gap-4 py-3 border-b border-border-weak-base last:border-none">
                  <div class="min-w-0 flex items-center gap-2">
                    <span class="text-14-regular text-text-strong truncate block">{item.name}</span>
                    <Tag>{language.t("model.custom.tag")}</Tag>
                  </div>
                  <div class="flex-shrink-0 flex items-center gap-2">
                    <IconButton
                      icon="pencil-line"
                      variant="ghost"
                      size="small"
                      onClick={() => handleEditCustom(index())}
                      aria-label={language.t("model.custom.edit.button")}
                    />
                    <IconButton
                      icon="trash"
                      variant="ghost"
                      size="small"
                      onClick={() => handleDeleteCustom(index())}
                      aria-label={language.t("model.custom.delete.button")}
                    />
                  </div>
                </div>
              )}
            </For>
          </div>
        </div>
      </Show>

      <Show when={models.custom.list().length === 0}>
        <div class="flex flex-col gap-4 max-w-[720px]">
          <div class="flex items-center justify-between">
            <div class="flex flex-col gap-1">
              <h3 class="text-14-medium text-text-strong">{language.t("model.custom.section.title")}</h3>
              <p class="text-12-regular text-text-weak">{language.t("model.custom.section.description")}</p>
            </div>
            <Button variant="ghost" size="small" icon="plus-small" onClick={handleAddCustom}>
              {language.t("model.custom.add.button")}
            </Button>
          </div>
        </div>
      </Show>
    </div>
  )
}
