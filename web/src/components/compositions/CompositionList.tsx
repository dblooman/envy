import { useState, useMemo, useEffect } from "react";
import {
  Search,
  Filter,
  Plus,
  Layers,
  CheckCircle2,
  Clock,
  AlertTriangle,
} from "lucide-react";
import { Composition } from "../../types/api";
import { CompositionCard } from "./CompositionCard";
import { CompositionDetailModal } from "./CompositionDetailModal";
import { UpdateCompositionDialog } from "./UpdateCompositionDialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { useEnvyApi } from "../../context/ApiContext";

interface CompositionListProps {
  onOpenCreate: () => void;
  selectedId?: string | null;
  onSelectedIdChange?: (id: string | null) => void;
}

export function CompositionList({
  onOpenCreate,
  selectedId,
  onSelectedIdChange,
}: CompositionListProps) {
  const { compositions, destroyComposition } = useEnvyApi();
  const initialQuery = new URLSearchParams(window.location.search);
  const [searchQuery, setSearchQuery] = useState(initialQuery.get("q") || "");
  const [phaseFilter, setPhaseFilter] = useState<string>(
    initialQuery.get("phase") || "active",
  );

  const [selectedComp, setSelectedComp] = useState<Composition | null>(null);
  const [updateModalOpen, setUpdateModalOpen] = useState(false);
  const [compToUpdate, setCompToUpdate] = useState<Composition | null>(null);
  const [destroyTarget, setDestroyTarget] = useState<Composition | null>(null);
  const [actionError, setActionError] = useState("");

  // Stats calculation
  const stats = useMemo(() => {
    const total = compositions.length;
    const ready = compositions.filter((c) => c.phase === "ready").length;
    const pending = compositions.filter(
      (c) => c.phase === "provisioning" || c.phase === "updating",
    ).length;
    const terminated = compositions.filter(
      (c) => c.phase === "destroyed",
    ).length;
    return { total, ready, pending, terminated };
  }, [compositions]);

  // Filtered items
  const filteredCompositions = useMemo(() => {
    return compositions.filter((comp) => {
      const matchesSearch =
        comp.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        comp.id.toLowerCase().includes(searchQuery.toLowerCase()) ||
        Object.entries(comp.overrides).some(([component, override]) =>
          `${component} ${override.image}`
            .toLowerCase()
            .includes(searchQuery.toLowerCase()),
        );

      if (!matchesSearch) return false;

      if (phaseFilter === "ready") return comp.phase === "ready";
      if (phaseFilter === "pending")
        return comp.phase === "provisioning" || comp.phase === "updating";
      if (phaseFilter === "terminated") return comp.phase === "destroyed";
      if (phaseFilter === "failed") return comp.phase === "failed";
      if (phaseFilter === "active") return comp.phase !== "destroyed";

      return true;
    });
  }, [compositions, searchQuery, phaseFilter]);

  const handleInspect = (comp: Composition) => {
    setSelectedComp(comp);
    onSelectedIdChange?.(comp.id);
  };

  const handleUpdate = (comp: Composition) => {
    setCompToUpdate(comp);
    setUpdateModalOpen(true);
  };

  const handleDestroy = async (comp: Composition) => {
    setDestroyTarget(comp);
  };

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (searchQuery) params.set("q", searchQuery);
    else params.delete("q");
    if (phaseFilter !== "active") params.set("phase", phaseFilter);
    else params.delete("phase");
    window.history.replaceState(
      {},
      "",
      `${window.location.pathname}${params.size ? `?${params}` : ""}`,
    );
  }, [searchQuery, phaseFilter]);

  return (
    <div className="space-y-6">
      {/* Metrics Row */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 sm:gap-4">
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs">
          <div className="flex items-center justify-between text-muted-foreground text-xs font-medium">
            <span>Total Previews</span>
            <Layers className="h-4 w-4" />
          </div>
          <div className="mt-2 text-2xl font-bold tracking-tight text-foreground font-mono">
            {stats.total}
          </div>
        </div>

        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs">
          <div className="flex items-center justify-between text-muted-foreground text-xs font-medium">
            <span>Active & Ready</span>
            <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
          </div>
          <div className="mt-2 text-2xl font-bold tracking-tight text-emerald-700 dark:text-emerald-400 font-mono">
            {stats.ready}
          </div>
        </div>

        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs">
          <div className="flex items-center justify-between text-muted-foreground text-xs font-medium">
            <span>In Progress</span>
            <Clock className="h-4 w-4 text-amber-600 dark:text-amber-400 animate-spin" />
          </div>
          <div className="mt-2 text-2xl font-bold tracking-tight text-amber-700 dark:text-amber-400 font-mono">
            {stats.pending}
          </div>
        </div>

        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs">
          <div className="flex items-center justify-between text-muted-foreground text-xs font-medium">
            <span>Tombstoned</span>
            <AlertTriangle className="h-4 w-4 text-zinc-400" />
          </div>
          <div className="mt-2 text-2xl font-bold tracking-tight text-muted-foreground font-mono">
            {stats.terminated}
          </div>
        </div>
      </div>

      {/* Search and Filters Bar */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search by preview name, ID, or image..."
            className="pl-9 text-xs sm:text-sm bg-card shadow-2xs"
          />
        </div>

        <div className="flex items-center gap-1.5 overflow-x-auto pb-1 sm:pb-0">
          <span className="text-xs text-muted-foreground mr-1 flex items-center gap-1">
            <Filter className="h-3.5 w-3.5" /> Filter:
          </span>
          {[
            { id: "all", label: "All" },
            { id: "active", label: "Active" },
            { id: "ready", label: "Ready" },
            { id: "pending", label: "In Progress" },
            { id: "terminated", label: "Terminated" },
            { id: "failed", label: "Failed" },
          ].map((f) => (
            <button
              key={f.id}
              onClick={() => setPhaseFilter(f.id)}
              className={`px-2.5 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer ${
                phaseFilter === f.id
                  ? "bg-primary text-primary-foreground shadow-2xs"
                  : "bg-muted/70 text-muted-foreground hover:bg-muted hover:text-foreground"
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Compositions Grid */}
      {filteredCompositions.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredCompositions.map((comp) => (
            <CompositionCard
              key={comp.id}
              composition={comp}
              onInspect={handleInspect}
              onUpdate={handleUpdate}
              onDestroy={handleDestroy}
            />
          ))}
        </div>
      ) : (
        <div className="text-center py-16 px-4 rounded-xl border border-dashed border-border bg-card">
          <Layers className="h-10 w-10 text-muted-foreground mx-auto mb-3 opacity-40" />
          <h3 className="font-semibold text-foreground text-base">
            No compositions found
          </h3>
          <p className="text-xs sm:text-sm text-muted-foreground max-w-sm mx-auto mt-1 mb-4">
            {searchQuery
              ? "Try refining your search query or reset filters."
              : "Launch your first temporary composition override on the staging baseline."}
          </p>
          <Button size="sm" onClick={onOpenCreate} className="gap-2 shadow-sm">
            <Plus className="h-4 w-4" />
            Launch Preview Composition
          </Button>
        </div>
      )}

      {/* Modals */}
      <CompositionDetailModal
        composition={
          compositions.find((c) => c.id === (selectedId || selectedComp?.id)) ??
          selectedComp
        }
        open={Boolean(selectedId || selectedComp)}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedComp(null);
            onSelectedIdChange?.(null);
          }
        }}
        onUpdate={handleUpdate}
        onDestroy={handleDestroy}
      />

      <UpdateCompositionDialog
        composition={compToUpdate}
        open={updateModalOpen}
        onOpenChange={setUpdateModalOpen}
      />
      {actionError && (
        <p
          role="alert"
          className="fixed bottom-4 right-4 z-50 rounded-lg border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800"
        >
          {actionError}
        </p>
      )}
      {destroyTarget && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="destroy-title"
          className="fixed inset-0 z-50 grid place-items-center bg-black/45 p-4"
        >
          <div className="w-full max-w-md rounded-xl border border-border bg-card p-5 shadow-xl">
            <h2 id="destroy-title" className="font-semibold">
              Destroy {destroyTarget.name}?
            </h2>
            <p className="mt-2 text-sm text-muted-foreground">
              Its preview hostname and owned workloads will be removed. The
              history remains available.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setDestroyTarget(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={async () => {
                  const target = destroyTarget;
                  setDestroyTarget(null);
                  setActionError("");
                  try {
                    await destroyComposition(target.id);
                  } catch (e) {
                    setActionError(
                      e instanceof Error
                        ? e.message
                        : "Failed to destroy composition",
                    );
                  }
                }}
              >
                Destroy composition
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
