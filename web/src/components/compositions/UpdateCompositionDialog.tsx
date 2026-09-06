import React, { useState, useEffect } from 'react'
import { RefreshCw, AlertCircle, ArrowUpRight } from 'lucide-react'
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
import { Composition } from '../../types/api'
import { useEnvyApi } from '../../context/ApiContext'

interface UpdateCompositionDialogProps {
  composition: Composition | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UpdateCompositionDialog({
  composition,
  open,
  onOpenChange,
}: UpdateCompositionDialogProps) {
  const { updateComposition } = useEnvyApi()
  const [image, setImage] = useState('')
  const [expectedGeneration, setExpectedGeneration] = useState<number>(1)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  useEffect(() => {
    if (composition) {
      setExpectedGeneration(composition.generation)
      const currentImg = composition.overrides['service-b']?.image || 'envy/service-b:v3'
      // Suggest opposite or next version
      setImage(currentImg === 'envy/service-b:v2' ? 'envy/service-b:v3' : 'envy/service-b:v2')
    }
  }, [composition])

  if (!composition) return null

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!image.trim()) {
      setFormError('Image tag cannot be empty')
      return
    }

    setSubmitting(true)
    setFormError(null)

    try {
      await updateComposition(composition.id, {
        expected_generation: expectedGeneration,
        overrides: {
          'service-b': {
            image: image.trim(),
          },
        },
      })
      onOpenChange(false)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to update image'
      setFormError(msg)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPortal>
        <DialogBackdrop />
        <DialogPopup className="max-w-md">
          <form onSubmit={handleSubmit}>
            <DialogHeader>
              <div className="flex items-center gap-2">
                <RefreshCw className="h-5 w-5 text-primary" />
                <DialogTitle>Rolling Image Update</DialogTitle>
              </div>
              <DialogDescription>
                Update <span className="font-semibold text-foreground">{composition.name}</span> while keeping its URL.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4 text-sm">
              {formError && (
                <div className="p-3 rounded-lg bg-red-950/80 border border-red-800 text-red-200 text-xs flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-red-400" />
                  <span>{formError}</span>
                </div>
              )}

              <div className="p-3 rounded-lg bg-secondary/60 border border-border text-xs space-y-1">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Current Image:</span>
                  <span className="font-mono text-foreground font-medium">
                    {composition.overrides['service-b']?.image || 'unknown'}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Current Generation:</span>
                  <span className="font-mono text-foreground">
                    Gen {composition.generation}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Stable URL:</span>
                  <span className="text-primary truncate max-w-[200px]" title={composition.endpoints.public.url}>
                    {composition.endpoints.public.url}
                  </span>
                </div>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">
                  Target Service Image Tag
                </label>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setImage('envy/service-b:v2')}
                    className={`text-xs ${image === 'envy/service-b:v2' ? 'border-primary bg-primary/10' : ''}`}
                  >
                    service-b:v2
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setImage('envy/service-b:v3')}
                    className={`text-xs ${image === 'envy/service-b:v3' ? 'border-primary bg-primary/10' : ''}`}
                  >
                    service-b:v3
                  </Button>
                </div>
                <Input
                  value={image}
                  onChange={(e) => setImage(e.target.value)}
                  placeholder="envy/service-b:v3"
                  required
                />
              </div>

              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-foreground">
                    Expected Generation
                  </label>
                  <span className="text-[11px] text-muted-foreground">
                    Optimistic concurrency guard
                  </span>
                </div>
                <Input
                  type="number"
                  min={1}
                  value={expectedGeneration}
                  onChange={(e) => setExpectedGeneration(parseInt(e.target.value) || 1)}
                  required
                />
              </div>

              <p className="text-[11px] text-muted-foreground">
                The URL, ID, and expiry remain stable. Ingress readiness will turn false until
                traffic to the new generation is verified through the Istio mesh.
              </p>
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
                {submitting ? 'Updating...' : (
                  <>
                    <ArrowUpRight className="h-4 w-4" />
                    Deploy Generation {expectedGeneration + 1}
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
