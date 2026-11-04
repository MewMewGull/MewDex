import type { HubProxySettings, SSHProfile } from "./localProtocol.ts";

// Fill the SSH connection while retaining the configured reverse listener,
// enable switch and any metadata saved by older clients.
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
