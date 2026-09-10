import { useState, useMemo } from "react";
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
}

export function CompositionList({ onOpenCreate }: CompositionListProps) {
  const { compositions, destroyComposition } = useEnvyApi();
  const [searchQuery, setSearchQuery] = useState("");
  const [phaseFilter, setPhaseFilter] = useState<string>("all");

  const [selectedComp, setSelectedComp] = useState<Composition | null>(null);
  const [detailModalOpen, setDetailModalOpen] = useState(false);
  const [updateModalOpen, setUpdateModalOpen] = useState(false);
  const [compToUpdate, setCompToUpdate] = useState<Composition | null>(null);

  // Stats calculation
  const stats = useMemo(() => {
    const total = compositions.length;
    const ready = compositions.filter((c) => c.phase === "ready").length;
    const pending = compositions.filter(
      (c) => c.phase === "provisioning" || c.phase === "updating",
    ).length;
    const terminated = compositions.filter(
      (c) => c.phase === "destroyed" || c.phase === "failed",
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
      if (phaseFilter === "terminated")
        return comp.phase === "destroyed" || comp.phase === "failed";

      return true;
    });
  }, [compositions, searchQuery, phaseFilter]);

  const handleInspect = (comp: Composition) => {
    setSelectedComp(comp);
    setDetailModalOpen(true);
  };

  const handleUpdate = (comp: Composition) => {
    setCompToUpdate(comp);
    setUpdateModalOpen(true);
  };

  const handleDestroy = async (comp: Composition) => {
    if (
      window.confirm(
        `Are you sure you want to destroy composition "${comp.name}"?`,
      )
    ) {
      try {
        await destroyComposition(comp.id);
      } catch (err: unknown) {
        alert(
          err instanceof Error ? err.message : "Failed to destroy composition",
        );
      }
    }
  };

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
            { id: "ready", label: "Ready" },
            { id: "pending", label: "In Progress" },
            { id: "terminated", label: "Terminated" },
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
        composition={compositions.find(c => c.id === selectedComp?.id) ?? selectedComp}
        open={detailModalOpen}
        onOpenChange={setDetailModalOpen}
        onUpdate={handleUpdate}
        onDestroy={handleDestroy}
      />

      <UpdateCompositionDialog
        composition={compToUpdate}
        open={updateModalOpen}
        onOpenChange={setUpdateModalOpen}
      />
    </div>
  );
}
