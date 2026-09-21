export function isDeploymentProfile(profile?: string): boolean {
  return profile === "deployment" || profile === "deployment-composite";
}
