import { useRef, useState } from "react";
import {
  Search,
  Plus,
  Layers3,
  CheckCircle2,
  Clock,
  TriangleAlert,
  LayoutGrid,
  List,
} from "lucide-react";
import { Composition } from "../../types/api";
import { CompositionCard } from "./CompositionCard";
import { CompositionDetailView, PreviewSection } from "./CompositionDetailView";
import { UpdateCompositionDialog } from "./UpdateCompositionDialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import {
  Dialog,
  DialogPortal,
  DialogBackdrop,
  DialogPopup,
  DialogTitle,
  DialogDescription,
} from "../ui/dialog";
import { useEnvyApi } from "../../context/ApiContext";

interface CompositionListProps {
  onOpenCreate: () => void;
  selectedId: string | null;
  onSelectedIdChange: (id: string | null) => void;
  query: string;
  onQueryChange: (query: string) => void;
}
const filters = [
  { id: "active", label: "Active" },
  { id: "all", label: "All" },
  { id: "ready", label: "Ready" },
  { id: "pending", label: "In progress" },
  { id: "failed", label: "Needs attention" },
  { id: "terminated", label: "Terminated" },
];
export function CompositionList({
  onOpenCreate,
  selectedId,
  onSelectedIdChange,
  query,
  onQueryChange,
}: CompositionListProps) {
  const { compositions, destroyComposition, loading, error, serverStatus } =
    useEnvyApi();
  const params = new URLSearchParams(query);
  const searchQuery = params.get("q") || "";
  const phaseFilter = filters.some((f) => f.id === params.get("phase"))
    ? params.get("phase")!
    : "active";
  const rows = params.get("view") === "rows";
  const requestedSection = params.get("section");
  const section: PreviewSection = [
    "Overview",
    "Changes",
    "Diagnostics",
    "Activity",
  ].includes(requestedSection || "")
    ? (requestedSection as PreviewSection)
    : "Overview";
  const setParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(query);
    if (value) next.set(key, value);
    else next.delete(key);
    onQueryChange(next.size ? `?${next}` : "");
  };
  const [compToUpdate, setCompToUpdate] = useState<Composition | null>(null);
  const [destroyTarget, setDestroyTarget] = useState<Composition | null>(null);
  const [destroying, setDestroying] = useState(false);
  const destroyInFlight = useRef(false);
  const [actionError, setActionError] = useState("");
  const selected = compositions.find((c) => c.id === selectedId);
  const filtered = compositions.filter((comp) => {
    const match = `${comp.name} ${comp.id} ${comp.project} ${Object.entries(
      comp.overrides,
    )
      .map(([id, override]) => `${id} ${override.image || ""}`)
      .join(" ")}`
      .toLowerCase()
      .includes(searchQuery.toLowerCase());
    if (!match) return false;
    if (phaseFilter === "ready") return comp.phase === "ready";
    if (phaseFilter === "pending")
      return ["created", "provisioning", "updating"].includes(comp.phase);
    if (phaseFilter === "terminated") return comp.phase === "destroyed";
    if (phaseFilter === "failed") return comp.phase === "failed";
    return phaseFilter === "all" || comp.phase !== "destroyed";
  });
  const requestDestroy = (composition: Composition) => {
    setActionError("");
    setDestroyTarget(composition);
  };
  return (
    <div className="space-y-6">
      {selectedId ? (
        selected ? (
          <CompositionDetailView
            key={selected.id}
            composition={selected}
            onBack={() => onSelectedIdChange(null)}
            onUpdate={setCompToUpdate}
            onDestroy={requestDestroy}
            section={section}
            onSectionChange={(next) =>
              setParam("section", next === "Overview" ? null : next)
            }
          />
        ) : (
          <section className="envy-empty" role="status">
            <Layers3 size={32} />
            <h1>
              {loading || serverStatus === "connecting"
                ? "Loading preview…"
                : "Preview unavailable"}
            </h1>
            <p>
              {error
                ? "Check the connection and refresh to load this preview."
                : "This preview may no longer exist or may not be accessible to your identity."}
            </p>
            <Button variant="outline" onClick={() => onSelectedIdChange(null)}>
              All previews
            </Button>
          </section>
        )
      ) : (
        <>
          <div className="envy-preview-summary">
            <span>
              <CheckCircle2 size={15} />
              <strong>
                {compositions.filter((c) => c.phase === "ready").length}
              </strong>{" "}
              ready
            </span>
            <span>
              <Clock size={15} />
              <strong>
                {
                  compositions.filter((c) =>
                    ["created", "provisioning", "updating"].includes(c.phase),
                  ).length
                }
              </strong>{" "}
              in progress
            </span>
            <span>
              <TriangleAlert size={15} />
              <strong>
                {compositions.filter((c) => c.phase === "failed").length}
              </strong>{" "}
              need attention
            </span>
            <span className="envy-summary-total">
              {compositions.length} total previews
            </span>
          </div>
          <div className="envy-preview-toolbar">
            <div className="relative flex-1 min-w-0">
              <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                aria-label="Search previews"
                value={searchQuery}
                onChange={(e) => setParam("q", e.target.value || null)}
                placeholder="Find a preview, component, or image…"
                className="pl-9 bg-card"
              />
            </div>
            <div
              className="envy-filter-group"
              role="group"
              aria-label="Filter previews"
            >
              {filters.map((filter) => (
                <button
                  key={filter.id}
                  onClick={() =>
                    setParam("phase", filter.id === "active" ? null : filter.id)
                  }
                  aria-pressed={phaseFilter === filter.id}
                >
                  {filter.label}
                </button>
              ))}
            </div>
            <div
              className="flex gap-1"
              role="group"
              aria-label="Preview layout"
            >
              <Button
                variant={rows ? "ghost" : "secondary"}
                size="icon"
                aria-label="Card layout"
                aria-pressed={!rows}
                onClick={() => setParam("view", null)}
              >
                <LayoutGrid />
              </Button>
              <Button
                variant={rows ? "secondary" : "ghost"}
                size="icon"
                aria-label="Row layout"
                aria-pressed={rows}
                onClick={() => setParam("view", "rows")}
              >
                <List />
              </Button>
            </div>
          </div>
          {filtered.length ? (
            <div className={rows ? "envy-preview-rows" : "envy-preview-grid"}>
              {filtered.map((comp) => (
                <CompositionCard
                  key={comp.id}
                  composition={comp}
                  onInspect={(c) => onSelectedIdChange(c.id)}
                  onUpdate={setCompToUpdate}
                  onDestroy={requestDestroy}
                />
              ))}
            </div>
          ) : (
            <section className="envy-empty">
              <span className="envy-empty-icon">
                <Layers3 size={32} />
              </span>
              <h2>
                {loading
                  ? "Loading previews…"
                  : compositions.length
                    ? "No previews match these filters"
                    : error || serverStatus === "disconnected"
                      ? "Connect to your workspace"
                      : "Your next change starts here"}
              </h2>
              <p>
                {compositions.length
                  ? "Try another component, image, or status."
                  : "Reuse your staging environment and give your changes a place of their own."}
              </p>
              {compositions.length ? (
                <Button
                  variant="outline"
                  onClick={() => {
                    const next = new URLSearchParams(query);
                    next.delete("q");
                    next.set("phase", "all");
                    onQueryChange(`?${next}`);
                  }}
                >
                  Clear filters
                </Button>
              ) : (
                <Button onClick={onOpenCreate}>
                  <Plus />
                  Create your first preview
                </Button>
              )}
            </section>
          )}
        </>
      )}
      <UpdateCompositionDialog
        composition={compToUpdate}
        open={Boolean(compToUpdate)}
        onOpenChange={(open) => {
          if (!open) setCompToUpdate(null);
        }}
      />
      <Dialog
        open={Boolean(destroyTarget)}
        onOpenChange={(open) => {
          if (!open && !destroyInFlight.current) setDestroyTarget(null);
        }}
      >
        <DialogPortal>
          <DialogBackdrop />
          <DialogPopup>
            <DialogTitle>Destroy {destroyTarget?.name}?</DialogTitle>
            <DialogDescription>
              Its preview hostname and owned workloads will be removed. The
              history remains available.
            </DialogDescription>
            {actionError && (
              <p role="alert" className="text-sm text-destructive">
                {actionError}
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-3">
              <Button
                variant="outline"
                disabled={destroying}
                onClick={() => setDestroyTarget(null)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={destroying}
                onClick={async () => {
                  if (!destroyTarget || destroyInFlight.current) return;
                  destroyInFlight.current = true;
                  setDestroying(true);
                  setActionError("");
                  try {
                    await destroyComposition(destroyTarget.id);
                    setDestroyTarget(null);
                  } catch (e) {
                    setActionError(
                      e instanceof Error
                        ? e.message
                        : "Failed to destroy preview",
                    );
                  } finally {
                    destroyInFlight.current = false;
                    setDestroying(false);
                  }
                }}
              >
                {destroying ? "Destroying…" : "Destroy preview"}
              </Button>
            </div>
          </DialogPopup>
        </DialogPortal>
      </Dialog>
    </div>
  );
}
