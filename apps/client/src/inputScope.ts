// Shared local-data scope. No token or model credential enters storage keys.
export function inputScope(baseURL: string, identityId: string): string {
  const url = new URL(baseURL);
  return JSON.stringify([
    url.origin,
    url.pathname.replace(/\/+$/, ""),
    identityId,
  ]);
}
