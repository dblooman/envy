import { Input } from "../ui/input";
import { Button } from "../ui/button";

export function TextField({
  label,
  value,
  onChange,
  type = "text",
  required = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
  required?: boolean;
}) {
  return (
    <label className="block space-y-1 text-sm">
      <span>{label}</span>
      <Input
        type={type}
        value={value}
        required={required}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

export interface Entry {
  key: string;
  value: string;
}
export function KeyValues({
  label,
  entries,
  onChange,
}: {
  label: string;
  entries: Entry[];
  onChange: (entries: Entry[]) => void;
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="text-sm font-medium">{label}</legend>
      {entries.map((entry, index) => (
        <div key={index} className="grid gap-2 sm:grid-cols-[1fr_2fr_auto]">
          <TextField
            label={`${label} name ${index + 1}`}
            value={entry.key}
            onChange={(key) =>
              onChange(
                entries.map((old, i) => (i === index ? { ...old, key } : old)),
              )
            }
          />
          <TextField
            label={`${label} value ${index + 1}`}
            value={entry.value}
            onChange={(value) =>
              onChange(
                entries.map((old, i) =>
                  i === index ? { ...old, value } : old,
                ),
              )
            }
          />
          <Button
            type="button"
            variant="ghost"
            aria-label={`Remove ${label} ${index + 1}`}
            onClick={() => onChange(entries.filter((_, i) => i !== index))}
          >
            Remove
          </Button>
        </div>
      ))}
      <Button
        type="button"
        variant="outline"
        onClick={() => onChange([...entries, { key: "", value: "" }])}
      >
        Add {label}
      </Button>
    </fieldset>
  );
}
export function entryMap(entries: Entry[]): Record<string, string> {
  const result: Record<string, string> = Object.create(null);
  for (const entry of entries) {
    if (!entry.key.trim() || Object.hasOwn(result, entry.key.trim()))
      throw new Error("Names must be nonempty and unique.");
    result[entry.key.trim()] = entry.value;
  }
  return result;
}
