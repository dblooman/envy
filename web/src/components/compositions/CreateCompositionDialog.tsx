import React, { useState } from 'react'
import { Rocket, Sparkles, AlertCircle } from 'lucide-react'
import {
  Dialog,
  DialogPortal,
  DialogBackdrop,
  DialogPopup,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '../ui/dialog'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { useEnvyApi } from '../../context/ApiContext'

interface CreateCompositionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: (id: string) => void
}

const PRESET_IMAGES = [
  { label: 'service-b:v2', image: 'envy/service-b:v2', desc: 'Standard v2 feature build' },
  { label: 'service-b:v3', image: 'envy/service-b:v3', desc: 'Updated v3 release build' },
]

export function CreateCompositionDialog({
  open,
  onOpenChange,
  onSuccess,
}: CreateCompositionDialogProps) {
  const { createComposition, projects, baselines } = useEnvyApi()
  const [name, setName] = useState('')
  const [overrideImage, setOverrideImage] = useState('envy/service-b:v2')
  const [ttl, setTtl] = useState('8h')
  const [idempotencyKey, setIdempotencyKey] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const handlePresetClick = (img: string) => {
    setOverrideImage(img)
  }

  const generateRandomName = () => {
    const adjectives = ['swift', 'stellar', 'agile', 'bright', 'turbo', 'crisp', 'silent']
    const nouns = ['preview', 'staging', 'slice', 'canary', 'feature', 'patch']
    const randAdj = adjectives[Math.floor(Math.random() * adjectives.length)]
    const randNoun = nouns[Math.floor(Math.random() * nouns.length)]
    const num = Math.floor(Math.random() * 900) + 100
    setName(`${randAdj}-${randNoun}-${num}`)
  }

  React.useEffect(() => {
    if (open && !name) {
      generateRandomName()
    }
  }, [open, name])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setFormError('Composition name is required')
      return
    }
    if (!overrideImage.trim()) {
      setFormError('Override image is required')
      return
    }

    setSubmitting(true)
    setFormError(null)

    try {
      const comp = await createComposition(
        {
          project: projects[0]?.id || 'demo',
          baseline: baselines[0]?.id || 'staging',
          name: name.trim(),
          overrides: {
            'service-b': {
              image: overrideImage.trim(),
            },
          },
          ttl: ttl || '8h',
        },
        idempotencyKey.trim() || undefined
      )

      onOpenChange(false)
      setName('')
      if (onSuccess) {
        onSuccess(comp.id)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to create composition'
      setFormError(msg)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPortal>
        <DialogBackdrop />
        <DialogPopup className="max-w-lg">
          <form onSubmit={handleSubmit}>
            <DialogHeader>
              <div className="flex items-center gap-2">
                <Rocket className="h-5 w-5 text-primary" />
                <DialogTitle>Create Preview Composition</DialogTitle>
              </div>
              <DialogDescription>
                Combine shared staging with selected workload overrides.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4 text-sm">
              {formError && (
                <div className="p-3 rounded-lg bg-red-950/80 border border-red-800 text-red-200 text-xs flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-red-400" />
                  <span>{formError}</span>
                </div>
              )}

              {/* Name */}
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-foreground">
                    Composition Name
                  </label>
                  <button
                    type="button"
                    onClick={generateRandomName}
                    className="text-[11px] text-primary hover:underline flex items-center gap-1 cursor-pointer"
                  >
                    <Sparkles className="h-3 w-3" /> Randomize
                  </button>
                </div>
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. pr-42-pricing-service"
                  required
                />
              </div>

              {/* Project & Baseline */}
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Project
                  </label>
                  <Input value="demo" disabled className="bg-muted text-muted-foreground" />
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Baseline
                  </label>
                  <Input value="staging" disabled className="bg-muted text-muted-foreground" />
                </div>
              </div>

              {/* Workload Overrides */}
              <div className="space-y-2 pt-1">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-foreground">
                    Override Component: <span className="text-primary font-mono">service-b</span>
                  </label>
                </div>

                <div className="grid grid-cols-2 gap-2 mb-2">
                  {PRESET_IMAGES.map((preset) => (
                    <button
                      key={preset.image}
                      type="button"
                      onClick={() => handlePresetClick(preset.image)}
                      className={`p-2.5 rounded-lg border text-left text-xs transition-all cursor-pointer ${
                        overrideImage === preset.image
                          ? 'border-primary bg-primary/10 text-foreground ring-1 ring-primary'
                          : 'border-border bg-card/60 text-muted-foreground hover:border-muted-foreground'
                      }`}
                    >
                      <div className="font-semibold text-foreground">{preset.label}</div>
                      <div className="text-[10px] text-muted-foreground truncate">{preset.desc}</div>
                    </button>
                  ))}
                </div>

                <div className="space-y-1">
                  <label className="text-[11px] text-muted-foreground">Custom Image Tag</label>
                  <Input
                    value={overrideImage}
                    onChange={(e) => setOverrideImage(e.target.value)}
                    placeholder="envy/service-b:v2"
                    required
                  />
                </div>
              </div>

              {/* TTL and Idempotency */}
              <div className="grid grid-cols-2 gap-3 pt-1">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    TTL (Lifetime)
                  </label>
                  <select
                    value={ttl}
                    onChange={(e) => setTtl(e.target.value)}
                    className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value="1h" className="bg-card text-foreground">1 hour</option>
                    <option value="4h" className="bg-card text-foreground">4 hours</option>
                    <option value="8h" className="bg-card text-foreground">8 hours (default)</option>
                    <option value="24h" className="bg-card text-foreground">24 hours (max)</option>
                  </select>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Idempotency Key (Optional)
                  </label>
                  <Input
                    value={idempotencyKey}
                    onChange={(e) => setIdempotencyKey(e.target.value)}
                    placeholder="e.g. pr-123-ci-run"
                  />
                </div>
              </div>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => onOpenChange(false)}
                disabled={submitting}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={submitting}
                className="bg-blue-600 hover:bg-blue-700 text-white gap-2"
              >
                {submitting ? (
                  <>
                    <span className="h-3.5 w-3.5 rounded-full border-2 border-white/30 border-t-white animate-spin" />
                    Deploying...
                  </>
                ) : (
                  <>
                    <Rocket className="h-3.5 w-3.5" />
                    Launch Preview
                  </>
                )}
              </Button>
            </DialogFooter>
          </form>
        </DialogPopup>
      </DialogPortal>
    </Dialog>
  )
}
