import { useEffect, useMemo, useRef, useState } from "react";
import { Download, Upload, RotateCcw } from "lucide-react";
import { apiClient } from "../../lib/api-client";
import {
  FrontendBindingView,
  Recipe,
  RecreateRecipeResult,
} from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

function newKey() {
  return (
    globalThis.crypto?.randomUUID?.() ||
    `web-${Date.now()}-${Math.random().toString(16).slice(2)}`
  );
}
export function RecipesView() {
  const { compositions, isDemoMode, refreshAll } = useEnvyApi();
  const [selected, setSelected] = useState("");
  const [bindings, setBindings] = useState<FrontendBindingView[]>([]);
  const [chosen, setChosen] = useState<string[]>([]);
  const [text, setText] = useState("");
  const [name, setName] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<RecreateRecipeResult | null>(null);
  const key = useRef(newKey());
  useEffect(() => {
    setBindings([]);
    setChosen([]);
    if (!selected || isDemoMode) return;
    const controller = new AbortController();
    apiClient
      .listFrontendBindings(selected, controller.signal)
      .then((p) => setBindings(p.items))
      .catch(() => {});
    return () => controller.abort();
  }, [selected, isDemoMode]);
  useEffect(() => {
    key.current = newKey();
    setResult(null);
  }, [text, name]);
  const recipe = useMemo(() => {
    try {
      return JSON.parse(text) as Recipe;
    } catch {
      return null;
    }
  }, [text]);
  async function exportRecipe() {
    if (!selected) return;
    setBusy(true);
    setMessage("");
    try {
      const selectedBindings = bindings
        .filter((b) =>
          chosen.includes(`${b.binding.frontend}/${b.binding.revision}`),
        )
        .map((b) => ({
          name: b.binding.frontend,
          revision: b.binding.revision,
        }));
      const out = await apiClient.exportRecipe(selected, selectedBindings);
      const json = JSON.stringify(out, null, 2);
      setText(json);
      const a = document.createElement("a");
      a.href = URL.createObjectURL(
        new Blob([json], { type: "application/json" }),
      );
      a.download = `envy-${selected}.recipe.json`;
      a.click();
      URL.revokeObjectURL(a.href);
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Export failed");
    } finally {
      setBusy(false);
    }
  }
  async function validate() {
    if (!recipe) {
      setMessage("Recipe must be valid JSON.");
      return;
    }
    setBusy(true);
    try {
      await apiClient.validateRecipe(recipe);
      setMessage("Recipe is valid and portable.");
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Validation failed");
    } finally {
      setBusy(false);
    }
  }
  async function recreate() {
    if (!recipe || !name.trim()) {
      setMessage("Choose a valid recipe and a new composition name.");
      return;
    }
    setBusy(true);
    try {
      const out = await apiClient.recreateRecipe(
        recipe,
        name.trim(),
        key.current,
      );
      setResult(out);
      setMessage(
        out.binding_errors.length
          ? "Composition created; some frontend bindings need retry."
          : "Composition and frontend bindings accepted.",
      );
      await refreshAll();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Recreation failed");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="grid gap-5 lg:grid-cols-[0.8fr_1.2fr]">
      <section className="space-y-4 rounded-xl border border-border bg-card p-5">
        <h2 className="font-semibold">Export an environment</h2>
        <p className="text-sm text-muted-foreground">
          Download reproducible workload and explicitly selected frontend
          intent. Published URLs and checks stay with the original environment.
        </p>
        <select
          aria-label="Composition"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
          className="h-9 w-full rounded-md border border-input bg-background px-3"
        >
          <option value="">Choose a composition</option>
          {compositions.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name} · Gen {c.generation}
            </option>
          ))}
        </select>
        {bindings.length > 0 && (
          <fieldset className="space-y-2 rounded border border-border p-3">
            <legend className="px-1 text-sm font-medium">
              Include frontend intent
            </legend>
            {bindings.map(({ binding: b }) => {
              const value = `${b.frontend}/${b.revision}`;
              return (
                <label key={value} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={chosen.includes(value)}
                    onChange={(e) =>
                      setChosen((old) =>
                        e.target.checked
                          ? [...old, value]
                          : old.filter((item) => item !== value),
                      )
                    }
                  />
                  <span>
                    {b.frontend} ·{" "}
                    <span className="font-mono text-xs">
                      {b.revision.slice(0, 10)}
                    </span>
                  </span>
                </label>
              );
            })}
          </fieldset>
        )}
        <Button
          onClick={() => void exportRecipe()}
          disabled={!selected || busy || isDemoMode}
        >
          <Download className="h-4 w-4" />
          Export recipe
        </Button>
        {isDemoMode && (
          <p className="text-sm text-muted-foreground">
            Recipe APIs are available in Live Mode.
          </p>
        )}
      </section>
      <section className="space-y-4 rounded-xl border border-border bg-card p-5">
        <div>
          <h2 className="font-semibold">Validate and recreate</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Paste or import a recipe, inspect it, then create a new composition
            using a retry-safe request.
          </p>
        </div>
        <label className="inline-flex cursor-pointer items-center gap-2 text-sm font-medium">
          <Upload className="h-4 w-4" />
          Import JSON
          <input
            className="sr-only"
            type="file"
            accept="application/json,.json"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (file) setText(await file.text());
            }}
          />
        </label>
        <textarea
          aria-label="Recipe JSON"
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={13}
          className="w-full rounded-md border border-input bg-background p-3 font-mono text-xs"
          placeholder="Paste envy/recipe-v1 JSON"
        />
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button
            variant="outline"
            onClick={() => void validate()}
            disabled={!text || busy}
          >
            Validate
          </Button>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="New composition name"
          />
          <Button
            onClick={() => void recreate()}
            disabled={!recipe || !name || busy || isDemoMode}
          >
            <RotateCcw className="h-4 w-4" />
            Recreate
          </Button>
        </div>
        {message && (
          <p role="status" className="text-sm">
            {message}
          </p>
        )}
        {result && (
          <div className="rounded border border-border p-3 text-sm">
            <p>
              Composition <strong>{result.composition.name}</strong> ·{" "}
              {result.composition.id}
            </p>
            <p>{result.bindings.length} frontend bindings created.</p>
            {result.binding_errors.map((error) => (
              <p key={error} className="text-amber-700">
                {error}
              </p>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
