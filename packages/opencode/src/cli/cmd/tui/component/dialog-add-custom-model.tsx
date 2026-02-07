import { createSignal, createMemo, Show } from "solid-js"
import { useSync } from "@tui/context/sync"
import { useSDK } from "@tui/context/sdk"
import { useDialog } from "@tui/ui/dialog"
import { useTheme } from "@tui/context/theme"
import { DialogPrompt } from "@tui/ui/dialog-prompt"
import { DialogSelect, type DialogSelectOption } from "@tui/ui/dialog-select"
import { DialogModel } from "./dialog-model"

export function DialogAddCustomModel() {
  const dialog = useDialog()
  const sdk = useSDK()
  const sync = useSync()
  const { theme } = useTheme()
  const [step, setStep] = createSignal<"provider" | "id" | "name" | "context" | "cost" | "confirm">("provider")
  const [providerID, setProviderID] = createSignal<string>("")
  const [modelID, setModelID] = createSignal<string>("")
  const [modelName, setModelName] = createSignal<string>("")
  const [contextWindow, setContextWindow] = createSignal<string>("")
  const [inputCost, setInputCost] = createSignal<string>("")
  const [outputCost, setOutputCost] = createSignal<string>("")

  const connectedProviders = () => sync.data.provider.filter((p) => sync.data.provider_next.connected.includes(p.id))

  const providerOptions = createMemo<DialogSelectOption<string>[]>(() =>
    connectedProviders().map((provider) => ({
      title: provider.name,
      value: provider.id,
      onSelect: () => handleProviderSelect(provider.id),
    })),
  )

  const handleProviderSelect = (id: string) => {
    setProviderID(id)
    setStep("id")
    dialog.replace(() => (
      <DialogPrompt
        title="Model ID"
        placeholder="e.g., gpt-5"
        description={() => <text fg={theme.textMuted}>The model identifier used by the provider</text>}
        onConfirm={(value) => {
          if (!value.trim()) {
            dialog.replace(() => <DialogAddCustomModel />)
            return
          }
          setModelID(value.trim())
          setStep("name")
          dialog.replace(() => (
            <DialogPrompt
              title="Display Name"
              placeholder="e.g., GPT-5 (Preview)"
              value={value.trim()}
              onConfirm={(nameValue) => {
                setModelName(nameValue.trim() || value.trim())
                setStep("context")
                dialog.replace(() => (
                  <DialogPrompt
                    title="Context Window"
                    placeholder="128000"
                    description={() => <text fg={theme.textMuted}>Maximum tokens in context</text>}
                    onConfirm={(ctxValue) => {
                      setContextWindow(ctxValue.trim())
                      setStep("cost")
                      dialog.replace(() => (
                        <DialogPrompt
                          title="Input Cost (per 1M tokens)"
                          placeholder="2.00"
                          description={() => (
                            <text fg={theme.textMuted}>Cost in USD per 1M input tokens (optional)</text>
                          )}
                          onConfirm={(costValue) => {
                            setInputCost(costValue.trim())
                            dialog.replace(() => (
                              <DialogPrompt
                                title="Output Cost (per 1M tokens)"
                                placeholder="10.00"
                                description={() => (
                                  <text fg={theme.textMuted}>Cost in USD per 1M output tokens (optional)</text>
                                )}
                                onConfirm={async (outCostValue) => {
                                  setOutputCost(outCostValue.trim())
                                  await saveCustomModel()
                                }}
                              />
                            ))
                          }}
                        />
                      ))
                    }}
                  />
                ))
              }}
            />
          ))
        }}
      />
    ))
  }

  const saveCustomModel = async () => {
    const provider = providerID()
    const id = modelID()
    const name = modelName()
    const context = parseInt(contextWindow()) || 128000
    const input = parseFloat(inputCost()) || 0
    const output = parseFloat(outputCost()) || 0

    if (!provider || !id) {
      dialog.clear()
      return
    }

    const updatedConfig = {
      provider: {
        [provider]: {
          models: {
            [id]: {
              id,
              name,
              cost: {
                input: input / 1000000,
                output: output / 1000000,
              },
              limit: {
                context,
                output: context / 4,
              },
            },
          },
        },
      },
    }

    const result = await sdk.client.global.config.update({
      config: updatedConfig,
    })

    if (result.error) {
      dialog.clear()
      return
    }

    // Refresh to get new provider data
    await sdk.client.instance.dispose()
    await sync.bootstrap()

    // Return to model dialog
    dialog.replace(() => <DialogModel providerID={provider} />)
  }

  return (
    <Show
      when={providerOptions().length > 0}
      fallback={
        <DialogSelect
          title="Add Custom Model"
          skipFilter
          options={[
            {
              title: "No providers connected",
              value: "none",
              disabled: true,
            },
          ]}
        />
      }
    >
      <DialogSelect title="Add Custom Model" skipFilter options={providerOptions()} />
    </Show>
  )
}
