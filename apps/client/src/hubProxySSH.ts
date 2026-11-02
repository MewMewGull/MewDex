import type { HubProxySettings, SSHProfile } from "./localProtocol.ts";

// Import just the SSH connection. The public URL, tunnel binding and enable
// switch remain the user's current choices, and every field stays editable.
export function fillSSHProfile(
  current: HubProxySettings,
  profile: SSHProfile,
): HubProxySettings {
  if (!profile.importable) throw new Error("此 SSH 条目需要在表单中手动配置。");
  return {
    ...current,
    host: profile.host,
    port: profile.port,
    user: profile.user,
    identityFile: profile.identityFile,
    knownHostsFile: profile.knownHostsFile,
  };
}
