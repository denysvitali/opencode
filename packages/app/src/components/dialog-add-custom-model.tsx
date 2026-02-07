import { Button } from "@opencode-ai/ui/button"
import { useDialog } from "@opencode-ai/ui/context/dialog"
import { Dialog } from "@opencode-ai/ui/dialog"
import { IconButton } from "@opencode-ai/ui/icon-button"
import { ProviderIcon } from "@opencode-ai/ui/provider-icon"
import { Select } from "@opencode-ai/ui/select"
import { TextField } from "@opencode-ai/ui/text-field"
import { createStore } from "solid-js/store"
import type { CustomModel } from "@/context/models"
import { useLanguage } from "@/context/language"
import { useModels } from "@/context/models"
import { useProviders } from "@/hooks/use-providers"
import type { IconName } from "@opencode-ai/ui/icons/provider"

type Props = {
  edit?: CustomModel & { index: number }
  onBack?: () => void
}

export function DialogAddCustomModel(props: Props) {
  const dialog = useDialog()
  const language = useLanguage()
  const models = useModels()
  const providers = useProviders()

  const connectedProviders = () => providers.connected()

  const [form, setForm] = createStore({
    id: props.edit?.id ?? "",
    providerID: props.edit?.providerID ?? connectedProviders()[0]?.id ?? "",
    name: props.edit?.name ?? "",
    contextWindow: props.edit?.limit.context ? String(props.edit.limit.context) : "",
    inputCost: props.edit?.cost.input ? String(props.edit.cost.input * 1000000) : "",
    outputCost: props.edit?.cost.output ? String(props.edit.cost.output * 1000000) : "",
    cacheReadCost: props.edit?.cost.cache_read ? String(props.edit.cost.cache_read * 1000000) : "",
    cacheWriteCost: props.edit?.cost.cache_write ? String(props.edit.cost.cache_write * 1000000) : "",
    maxOutput: props.edit?.limit.output ? String(props.edit.limit.output) : "",
  })

  const [errors, setErrors] = createStore({
    id: undefined as string | undefined,
    providerID: undefined as string | undefined,
    name: undefined as string | undefined,
    contextWindow: undefined as string | undefined,
    inputCost: undefined as string | undefined,
    outputCost: undefined as string | undefined,
  })

  const validate = () => {
    const id = form.id.trim()
    const providerID = form.providerID.trim()
    const name = form.name.trim()
    const contextWindow = Number(form.contextWindow)
    const inputCost = Number(form.inputCost)
    const outputCost = Number(form.outputCost)

    const idError = !id ? language.t("model.custom.error.id.required") : undefined
    const providerError = !providerID ? language.t("model.custom.error.provider.required") : undefined
    const nameError = !name ? language.t("model.custom.error.name.required") : undefined
    const contextError =
      !form.contextWindow || contextWindow <= 0 ? language.t("model.custom.error.contextWindow.required") : undefined
    const inputCostError = form.inputCost && inputCost < 0 ? language.t("model.custom.error.cost.negative") : undefined
    const outputCostError =
      form.outputCost && outputCost < 0 ? language.t("model.custom.error.cost.negative") : undefined

    const existingIndex = models.custom.findIndex(id, providerID)
    const duplicateError =
      existingIndex >= 0 && (!props.edit || existingIndex !== props.edit.index)
        ? language.t("model.custom.error.duplicate")
        : undefined

    setErrors({
      id: idError ?? duplicateError,
      providerID: providerError,
      name: nameError,
      contextWindow: contextError,
      inputCost: inputCostError,
      outputCost: outputCostError,
    })

    if (idError || duplicateError || providerError || nameError || contextError || inputCostError || outputCostError) {
      return null
    }

    const cacheReadCost = form.cacheReadCost ? Number(form.cacheReadCost) : undefined
    const cacheWriteCost = form.cacheWriteCost ? Number(form.cacheWriteCost) : undefined
    const maxOutput = form.maxOutput ? Number(form.maxOutput) : undefined

    return {
      id,
      providerID,
      name,
      cost: {
        input: inputCost / 1000000,
        output: outputCost / 1000000,
        ...(cacheReadCost !== undefined ? { cache_read: cacheReadCost / 1000000 } : {}),
        ...(cacheWriteCost !== undefined ? { cache_write: cacheWriteCost / 1000000 } : {}),
      },
      limit: {
        context: contextWindow,
        ...(maxOutput !== undefined ? { output: maxOutput } : {}),
      },
    } as CustomModel
  }

  const handleSubmit = (e: Event) => {
    e.preventDefault()
    const model = validate()
    if (!model) return

    if (props.edit) {
      models.custom.update(props.edit.index, model)
    } else {
      models.custom.add(model)
    }

    dialog.close()
  }

  const handleCancel = () => {
    if (props.onBack) {
      props.onBack()
    } else {
      dialog.close()
    }
  }

  return (
    <Dialog
      title={
        props.onBack ? (
          <IconButton
            tabIndex={-1}
            icon="arrow-left"
            variant="ghost"
            onClick={props.onBack}
            aria-label={language.t("common.goBack")}
          />
        ) : undefined
      }
      transition
    >
      <div class="flex flex-col gap-6 px-2.5 pb-3">
        <div class="px-2.5 flex gap-4 items-center">
          <ProviderIcon id="synthetic" class="size-5 shrink-0 icon-strong-base" />
          <div class="text-16-medium text-text-strong">
            {props.edit ? language.t("model.custom.edit.title") : language.t("model.custom.add.title")}
          </div>
        </div>

        <form onSubmit={handleSubmit} class="px-2.5 pb-6 flex flex-col gap-6">
          <div class="flex flex-col gap-4">
            <TextField
              autofocus
              label={language.t("model.custom.field.id.label")}
              placeholder={language.t("model.custom.field.id.placeholder")}
              description={language.t("model.custom.field.id.description")}
              value={form.id}
              onChange={setForm.bind(null, "id")}
              validationState={errors.id ? "invalid" : undefined}
              error={errors.id}
            />

            <Select
              options={connectedProviders().map((p) => ({ value: p.id, label: p.name }))}
              current={connectedProviders()
                .map((p) => ({ value: p.id, label: p.name }))
                .find((o) => o.value === form.providerID)}
              value={(x) => x.value}
              label={(x) => x.label}
              onSelect={(v) => v && setForm("providerID", v.value)}
              validationState={errors.providerID ? "invalid" : undefined}
              error={errors.providerID}
            >
              {(item) => (
                <div class="flex items-center gap-2">
                  <ProviderIcon id={item?.value as IconName} class="size-4" />
                  <span>{item?.label}</span>
                </div>
              )}
            </Select>

            <TextField
              label={language.t("model.custom.field.name.label")}
              placeholder={language.t("model.custom.field.name.placeholder")}
              value={form.name}
              onChange={setForm.bind(null, "name")}
              validationState={errors.name ? "invalid" : undefined}
              error={errors.name}
            />

            <TextField
              label={language.t("model.custom.field.contextWindow.label")}
              placeholder={language.t("model.custom.field.contextWindow.placeholder")}
              description={language.t("model.custom.field.contextWindow.description")}
              type="number"
              value={form.contextWindow}
              onChange={setForm.bind(null, "contextWindow")}
              validationState={errors.contextWindow ? "invalid" : undefined}
              error={errors.contextWindow}
            />

            <div class="flex flex-col gap-3">
              <label class="text-12-medium text-text-weak">{language.t("model.custom.field.cost.label")}</label>
              <div class="flex gap-3">
                <TextField
                  label={language.t("model.custom.field.inputCost.label")}
                  placeholder={language.t("model.custom.field.inputCost.placeholder")}
                  type="number"
                  step="0.01"
                  value={form.inputCost}
                  onChange={setForm.bind(null, "inputCost")}
                  validationState={errors.inputCost ? "invalid" : undefined}
                  error={errors.inputCost}
                />
                <TextField
                  label={language.t("model.custom.field.outputCost.label")}
                  placeholder={language.t("model.custom.field.outputCost.placeholder")}
                  type="number"
                  step="0.01"
                  value={form.outputCost}
                  onChange={setForm.bind(null, "outputCost")}
                  validationState={errors.outputCost ? "invalid" : undefined}
                  error={errors.outputCost}
                />
              </div>
            </div>

            <div class="flex flex-col gap-3">
              <label class="text-12-medium text-text-weak">{language.t("model.custom.field.cacheCost.label")}</label>
              <div class="flex gap-3">
                <TextField
                  label={language.t("model.custom.field.cacheReadCost.label")}
                  placeholder={language.t("model.custom.field.cacheReadCost.placeholder")}
                  type="number"
                  step="0.01"
                  value={form.cacheReadCost}
                  onChange={setForm.bind(null, "cacheReadCost")}
                />
                <TextField
                  label={language.t("model.custom.field.cacheWriteCost.label")}
                  placeholder={language.t("model.custom.field.cacheWriteCost.placeholder")}
                  type="number"
                  step="0.01"
                  value={form.cacheWriteCost}
                  onChange={setForm.bind(null, "cacheWriteCost")}
                />
              </div>
            </div>

            <TextField
              label={language.t("model.custom.field.maxOutput.label")}
              placeholder={language.t("model.custom.field.maxOutput.placeholder")}
              description={language.t("model.custom.field.maxOutput.description")}
              type="number"
              value={form.maxOutput}
              onChange={setForm.bind(null, "maxOutput")}
            />
          </div>

          <div class="flex gap-3">
            <Button type="button" variant="ghost" onClick={handleCancel}>
              {language.t("common.cancel")}
            </Button>
            <Button type="submit" size="large" variant="primary">
              {props.edit ? language.t("common.save") : language.t("common.add")}
            </Button>
          </div>
        </form>
      </div>
    </Dialog>
  )
}
