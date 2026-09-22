export const activityActions: Record<string, string> = {
  "github.preview.reconcile": "Reconcile pull request preview",
  "github.webhook.receive": "Receive GitHub event",
  "build.report": "Report build",
  "composition.create": "Create preview",
  "composition.update": "Update preview",
  "composition.destroy": "Remove preview",
};
export function activityLabel(action: string) {
  return activityActions[action] || action;
}
